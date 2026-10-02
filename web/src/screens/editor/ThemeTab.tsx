import { useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { COLOR_KEYS, api, errorMessage, type BackgroundKind, type CatalogTheme, type Theme, type ThemeColors } from "../../api";
import { useCatalog } from "../../catalog";
import { Icon } from "../../components/Icon";
import { ImageField, Notice, SelectField, Spinner, Switch, uploadUrl } from "../../components/ui";
import { contrastRatio, normalizeHex } from "../../lib/validate";
import type { EditorProps } from "./types";
import { ProBadge, UpgradeNote } from "../../components/Pro";
import { planAllows, usePlan } from "../../plan";
import { CustomCSSSection, FontsSection } from "./ProThemeSections";

const COLOR_LABELS: Record<keyof ThemeColors, string> = {
  bg: "Background",
  surface: "Surface",
  text: "Text",
  muted: "Secondary text",
  accent: "Accent",
  button_bg: "Button",
  button_text: "Button text",
};

const SAVE_DELAY = 600;

type SaveStatus =
  | { kind: "saved" }
  | { kind: "pending" }
  | { kind: "saving" }
  | { kind: "incomplete" }
  | { kind: "error"; message: string };

function withDefaults(theme: Theme): Theme {
  return { ...theme, background: theme.background ?? { kind: "solid" } };
}

export function ThemeTab({ page, setPage, saved }: EditorProps) {
  const catalog = useCatalog();
  const [draft, setDraft] = useState<Theme>(() => withDefaults(page.theme));
  const [status, setStatus] = useState<SaveStatus>({ kind: "saved" });
  const lastSaved = useRef(JSON.stringify(withDefaults(page.theme)));
  const inFlight = useRef(false);
  const [retry, setRetry] = useState(0);

  // Debounced autosave: whenever the draft differs from what was last saved.
  useEffect(() => {
    const json = JSON.stringify(draft);
    if (json === lastSaved.current) {
      setStatus((s) => (s.kind === "error" ? s : { kind: "saved" }));
      return;
    }
    if (draft.background.kind === "image" && !draft.background.image_upload_id) {
      setStatus({ kind: "incomplete" });
      return;
    }
    setStatus({ kind: "pending" });
    const t = window.setTimeout(async () => {
      if (inFlight.current) {
        setRetry((r) => r + 1);
        return;
      }
      inFlight.current = true;
      setStatus({ kind: "saving" });
      try {
        const next = await api.updatePage(page.id, { theme: draft });
        lastSaved.current = json;
        setPage((p) => ({ ...p, theme: next.theme, updated_at: next.updated_at }));
        setStatus({ kind: "saved" });
        saved();
      } catch (err) {
        setStatus({ kind: "error", message: errorMessage(err) });
      } finally {
        inFlight.current = false;
      }
    }, SAVE_DELAY);
    return () => window.clearTimeout(t);
  }, [draft, page.id, setPage, saved, retry]);

  const update = (patch: Partial<Theme>) => setDraft((d) => ({ ...d, ...patch }));
  const setColor = (key: keyof ThemeColors, value: string) => setDraft((d) => ({ ...d, colors: { ...d.colors, [key]: value } }));
  const applyPreset = (t: CatalogTheme) =>
    setDraft((d) => ({
      ...d,
      preset: t.key,
      colors: { ...t.colors },
      font: t.font,
      button_style: t.button_style,
      button_shape: t.button_shape,
      layout: t.layout,
      background: { ...t.background },
    }));

  const textContrast = contrastRatio(draft.colors.text, draft.colors.bg);
  const buttonContrast = contrastRatio(draft.colors.button_text, draft.colors.button_bg);
  const lowContrast = [
    textContrast !== null && textContrast < 4.5 ? "text on the background" : null,
    buttonContrast !== null && buttonContrast < 4.5 ? "button text on buttons" : null,
  ].filter(Boolean);

  return (
    <div className="stack-lg theme-tab">
      <div className="theme-status" aria-live="polite">
        {status.kind === "saving" && (
          <span className="muted small">
            <Spinner label="Saving" /> Saving…
          </span>
        )}
        {status.kind === "pending" && <span className="muted small">Unsaved changes…</span>}
        {status.kind === "incomplete" && <span className="muted small">Upload a background image to save your changes.</span>}
        {status.kind === "saved" && (
          <span className="muted small">
            <Icon name="check" size={14} /> Changes save automatically
          </span>
        )}
        {status.kind === "error" && (
          <Notice
            tone="danger"
            action={
              <button type="button" className="btn btn-sm" onClick={() => setRetry((r) => r + 1)}>
                Retry
              </button>
            }
          >
            Couldn't save theme: {status.message}
          </Notice>
        )}
      </div>

      <section className="card section" aria-labelledby="presets-heading">
        <h2 id="presets-heading">Presets</h2>
        <p className="muted">Start from a preset, then adjust anything below.</p>
        <ul className="preset-grid">
          {catalog.themes.map((t) => (
            <li key={t.key}>
              <button type="button" className="preset-card" aria-pressed={draft.preset === t.key} onClick={() => applyPreset(t)}>
                <PresetSwatch theme={t} />
                <span className="preset-name">{t.name}</span>
              </button>
            </li>
          ))}
        </ul>
      </section>

      <section className="card section" aria-labelledby="colors-heading">
        <h2 id="colors-heading">Colors</h2>
        <div className="color-grid">
          {COLOR_KEYS.map((k) => (
            <ColorField key={k} label={COLOR_LABELS[k]} value={draft.colors[k]} onChange={(v) => setColor(k, v)} />
          ))}
        </div>
        {lowContrast.length > 0 && (
          <p className="contrast-warning">
            Low contrast for {lowContrast.join(" and ")}. Some visitors may find it hard to read.
          </p>
        )}
      </section>

      <section className="card section" aria-labelledby="style-heading">
        <h2 id="style-heading">Style</h2>
        <div className="form-grid">
          <SelectField label="Font" value={draft.font} onChange={(font) => update({ font })} options={catalog.fonts} />
          <SelectField label="Layout" value={draft.layout} onChange={(layout) => update({ layout })} options={catalog.layouts} />
          <SelectField
            label="Button style"
            value={draft.button_style}
            onChange={(button_style) => update({ button_style })}
            options={catalog.button_styles}
          />
          <SelectField
            label="Button shape"
            value={draft.button_shape}
            onChange={(button_shape) => update({ button_shape })}
            options={catalog.button_shapes}
          />
        </div>
      </section>

      <BackgroundSection theme={draft} onChange={(background) => update({ background })} />

      <FontsSection value={draft.font_uploads} onChange={(font_uploads) => update({ font_uploads })} />

      <FooterSection hidden={draft.hide_branding === true} onChange={(hide_branding) => update({ hide_branding })} />

      <CustomCSSSection page={page} setPage={setPage} saved={saved} />
    </div>
  );
}

function PresetSwatch({ theme }: { theme: CatalogTheme }) {
  const c = theme.colors;
  const radius = theme.button_shape === "pill" ? 99 : theme.button_shape === "rounded" ? 4 : 0;
  const outline = theme.button_style === "outline";
  const style = { background: c.bg, color: c.text } as CSSProperties;
  const button: CSSProperties = {
    background: outline ? "transparent" : c.button_bg,
    border: `1px solid ${outline ? c.accent : c.button_bg}`,
    borderRadius: radius,
  };
  return (
    <span className="swatch" style={style} aria-hidden="true">
      <span className="swatch-avatar" style={{ background: c.accent }} />
      <span className="swatch-line" style={{ background: c.text }} />
      <span className="swatch-line short" style={{ background: c.muted }} />
      <span className="swatch-button" style={button}>
        <span style={{ background: outline ? c.accent : c.button_text }} />
      </span>
      <span className="swatch-button" style={button}>
        <span style={{ background: outline ? c.accent : c.button_text }} />
      </span>
    </span>
  );
}

function ColorField({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  const id = useId();
  const [text, setText] = useState(value);
  const [invalid, setInvalid] = useState(false);
  useEffect(() => {
    setText(value);
    setInvalid(false);
  }, [value]);
  return (
    <div className="color-field">
      <label htmlFor={`${id}-hex`}>{label}</label>
      <div className="color-inputs">
        <input
          type="color"
          value={normalizeHex(value) ?? "#000000"}
          onChange={(e) => onChange(e.target.value)}
          aria-label={`${label} color picker`}
        />
        <input
          id={`${id}-hex`}
          className="hex-input"
          value={text}
          spellCheck={false}
          maxLength={7}
          aria-invalid={invalid || undefined}
          onChange={(e) => {
            setText(e.target.value);
            const n = normalizeHex(e.target.value);
            setInvalid(!n);
            if (n && e.target.value.replace("#", "").length === 6) onChange(n);
          }}
          onBlur={() => {
            const n = normalizeHex(text);
            if (n) onChange(n);
            else {
              setText(value);
              setInvalid(false);
            }
          }}
        />
      </div>
    </div>
  );
}

function BackgroundSection({ theme, onChange }: { theme: Theme; onChange: (bg: Theme["background"]) => void }) {
  const bg = theme.background;
  const name = useId();
  const kinds: { key: BackgroundKind; label: string }[] = [
    { key: "solid", label: "Solid" },
    { key: "gradient", label: "Gradient" },
    { key: "image", label: "Image" },
  ];
  return (
    <section className="card section" aria-labelledby="bg-heading">
      <h2 id="bg-heading">Background</h2>
      <fieldset className="segmented">
        <legend className="visually-hidden">Background type</legend>
        {kinds.map((k) => (
          <label key={k.key} className="segmented-option">
            <input
              type="radio"
              name={name}
              value={k.key}
              checked={bg.kind === k.key}
              onChange={() =>
                onChange({
                  ...bg,
                  kind: k.key,
                  color2: k.key === "gradient" ? bg.color2 || theme.colors.accent : bg.color2,
                })
              }
            />
            <span>{k.label}</span>
          </label>
        ))}
      </fieldset>
      {bg.kind === "solid" && <p className="muted small">Uses the Background color above.</p>}
      {bg.kind === "gradient" && (
        <div className="gradient-row">
          <span
            className="gradient-preview"
            aria-hidden="true"
            style={{ background: `linear-gradient(180deg, ${theme.colors.bg}, ${bg.color2 || theme.colors.accent})` }}
          />
          <ColorField
            label="Second color"
            value={bg.color2 || theme.colors.accent}
            onChange={(color2) => onChange({ ...bg, color2 })}
          />
        </div>
      )}
      {bg.kind === "image" && (
        <>
          <ImageField
            label="Background image"
            shape="wide"
            required
            imageUrl={bg.image_upload_id ? uploadUrl(bg.image_upload_id) : null}
            onChange={(up) => onChange({ ...bg, image_upload_id: up ? up.id : undefined })}
          />
          {!bg.image_upload_id && <p className="muted small">Until you upload an image, the Background color is used.</p>}
        </>
      )}
    </section>
  );
}

function FooterSection({ hidden, onChange }: { hidden: boolean; onChange: (hidden: boolean) => void }) {
  const { plan } = usePlan();
  const allowed = planAllows(plan, "remove_branding");
  return (
    <section className="card section" aria-labelledby="footer-heading">
      <div className="section-row">
        <div className="section-intro">
          <h2 id="footer-heading">
            Footer {!allowed && <ProBadge />}
          </h2>
          <p className="muted small">Remove the “Made with Kork” link at the bottom of your page. Share, Report, Terms and Privacy stay.</p>
        </div>
        {/* A lapsed Pro page can still switch it off; turning it on needs Pro. */}
        <Switch checked={hidden} onChange={onChange} label="Hide the footer credit" disabled={!allowed && !hidden} />
      </div>
      {!allowed && (
        <UpgradeNote>
          {hidden ? "Your page shows the footer credit again until you have Pro." : "Hiding the footer credit is part of Pro."}
        </UpgradeNote>
      )}
    </section>
  );
}
