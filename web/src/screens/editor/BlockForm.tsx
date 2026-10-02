import { Fragment, useState, type FormEvent } from "react";
import { ApiError, errorMessage, type BlockData, type BlockType, type CatalogService } from "../../api";
import { useCatalog } from "../../catalog";
import { ImageField, Notice, Spinner, TextField, uploadUrl } from "../../components/ui";
import { blockFields, cleanBlockData, missingRequired, type FieldSpec } from "../../lib/blocks";
import { LinkPreviewHelper } from "./LinkPreviewHelper";
import { ServiceMonogram } from "./ServicePicker";

interface BlockFormProps {
  type: BlockType;
  service?: CatalogService;
  initial?: BlockData;
  submitLabel: string;
  onSubmit: (data: BlockData) => Promise<void>;
  onCancel: () => void;
  onChangeService?: () => void;
}

export function BlockForm({ type, service, initial = {}, submitLabel, onSubmit, onCancel, onChangeService }: BlockFormProps) {
  const catalog = useCatalog();
  const fields = blockFields(type, service);
  const [data, setData] = useState<BlockData>(() => {
    const d: BlockData = { ...initial };
    if (type === "service" && service?.input === "handle" && typeof d.value === "string") {
      // Values are stored without the prefix; the prefix is shown as an adornment.
      d.value = d.value.startsWith(service.prefix) && service.prefix ? d.value.slice(service.prefix.length) : d.value;
    }
    return d;
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [general, setGeneral] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const set = (key: string, value: string | boolean | undefined) => {
    setData((d) => ({ ...d, [key]: value }));
    if (errors[key]) setErrors((e) => ({ ...e, [key]: "" }));
  };

  async function submit(e: FormEvent) {
    e.preventDefault();
    const missing = missingRequired(fields, data);
    if (Object.keys(missing).length) {
      setErrors(missing);
      focusFirstError(e.currentTarget as HTMLFormElement);
      return;
    }
    setBusy(true);
    setGeneral(null);
    setErrors({});
    try {
      await onSubmit(cleanBlockData(type, data, service));
    } catch (err) {
      if (err instanceof ApiError && err.field && fields.some((f) => f.key === err.field)) {
        setErrors({ [err.field]: err.message });
      } else {
        setGeneral(errorMessage(err));
      }
      setBusy(false);
    }
  }

  const embedHint =
    type === "embed" && catalog.embed_providers.length ? `Supported: ${catalog.embed_providers.join(", ")}.` : undefined;

  return (
    <form className="block-form" onSubmit={submit} noValidate>
      {type === "service" && service && (
        <div className="block-form-service">
          <ServiceMonogram service={service} size={40} />
          <div>
            <strong>{service.name}</strong>
            {service.copy_only && <p className="muted small">Visitors get a button that copies this value.</p>}
          </div>
          {onChangeService && (
            <button type="button" className="btn btn-sm btn-ghost" onClick={onChangeService}>
              Change service
            </button>
          )}
        </div>
      )}
      {type === "service" && !service && (
        <Notice tone="warning">This service is no longer in the catalog. You can still change the value or delete the block.</Notice>
      )}
      {type === "divider" && <p className="muted">Dividers have no settings.</p>}
      {general && <Notice tone="danger">{general}</Notice>}
      {fields.map((f) => (
        <Fragment key={f.key}>
          <FieldInput
            spec={f}
            value={data[f.key]}
            error={errors[f.key]}
            prefix={type === "service" && f.key === "value" && service?.input === "handle" ? service.prefix : undefined}
            hint={f.key === "url" && type === "embed" ? embedHint : f.hint}
            onChange={(v) => set(f.key, v)}
          />
          {type === "link" && f.key === "url" && (
            <LinkPreviewHelper
              url={str(data.url)}
              title={str(data.title)}
              subtitle={str(data.subtitle)}
              thumbnailId={str(data.thumbnail_upload_id)}
              onUse={set}
            />
          )}
        </Fragment>
      ))}
      <div className="form-actions">
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? <Spinner label="Saving" /> : null}
          {submitLabel}
        </button>
        <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
      </div>
    </form>
  );
}

const str = (v: string | boolean | undefined) => (typeof v === "string" ? v : "");

function focusFirstError(form: HTMLFormElement) {
  window.requestAnimationFrame(() => {
    const el = form.querySelector<HTMLElement>("[aria-invalid='true'], .has-error button");
    el?.focus();
  });
}

function FieldInput({
  spec,
  value,
  error,
  prefix,
  hint,
  onChange,
}: {
  spec: FieldSpec;
  value: string | boolean | undefined;
  error?: string;
  prefix?: string;
  hint?: string;
  onChange: (value: string | boolean | undefined) => void;
}) {
  if (spec.kind === "checkbox") {
    return (
      <div className="field">
        <label className="checkbox">
          <input type="checkbox" checked={value === true} onChange={(e) => onChange(e.target.checked)} />
          <span>{spec.label}</span>
        </label>
        {error && <p className="field-error">{error}</p>}
      </div>
    );
  }
  if (spec.kind === "image") {
    const id = typeof value === "string" && value ? value : null;
    return (
      <ImageField
        label={spec.label}
        required={spec.required}
        imageUrl={id ? uploadUrl(id) : null}
        error={error}
        shape={spec.key === "thumbnail_upload_id" ? "square" : "wide"}
        onChange={(up) => onChange(up ? up.id : undefined)}
      />
    );
  }
  const str = typeof value === "string" ? value : "";
  const typeMap = { text: "text", textarea: "text", url: "url", email: "email", tel: "tel" } as const;
  return (
    <TextField
      label={spec.label}
      value={str}
      onChange={onChange}
      type={typeMap[spec.kind]}
      inputMode={spec.kind === "url" ? "url" : spec.kind === "email" ? "email" : spec.kind === "tel" ? "tel" : undefined}
      multiline={spec.kind === "textarea"}
      required={spec.required}
      placeholder={spec.placeholder}
      maxLength={spec.maxLength}
      prefix={prefix}
      hint={hint}
      error={error}
      spellCheck={spec.kind === "text" || spec.kind === "textarea" ? undefined : false}
    />
  );
}
