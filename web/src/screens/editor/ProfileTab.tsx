import { useState, type FormEvent } from "react";
import { ApiError, api, errorMessage } from "../../api";
import { useAuth } from "../../auth";
import { SlugStatusBadge, slugStatusError } from "../../components/SlugStatus";
import { ImageField, Notice, Spinner, Switch, TextField } from "../../components/ui";
import { useSlugCheck } from "../../lib/useSlugCheck";
import { normalizeSlug } from "../../lib/validate";
import type { EditorProps } from "./types";

export function ProfileTab(props: EditorProps) {
  return (
    <div className="stack-lg">
      <PublishSection {...props} />
      <DetailsSection {...props} />
      <AddressSection {...props} />
    </div>
  );
}

function PublishSection({ page, setPage, saved }: EditorProps) {
  const { user } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ code: string; message: string } | null>(null);
  const [resend, setResend] = useState<"idle" | "sending" | "sent">("idle");

  async function setPublished(published: boolean) {
    setBusy(true);
    setError(null);
    try {
      const next = await api.updatePage(page.id, { published });
      setPage(() => next);
      saved();
    } catch (err) {
      setError({ code: err instanceof ApiError ? err.code : "error", message: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  }

  const unverified = error?.code === "email_unverified" || (user && !user.email_verified && !page.published);

  return (
    <section className="card section" aria-labelledby="publish-heading">
      <div className="section-row">
        <div className="section-intro">
          <h2 id="publish-heading">Publishing</h2>
          <p className="muted">
            {page.published
              ? "Your page is live. Anyone with the link can see it."
              : "Your page is a draft. Only you can see it, in the preview."}
          </p>
        </div>
        <Switch
          checked={page.published}
          disabled={busy || page.suspended}
          onChange={(v) => void setPublished(v)}
          label={page.published ? "Published" : "Publish"}
        />
      </div>
      {error?.code === "email_unverified" ? (
        <Notice
          tone="warning"
          title="Confirm your email to publish"
          action={
            <button
              type="button"
              className="btn btn-sm"
              disabled={resend !== "idle"}
              onClick={async () => {
                setResend("sending");
                try {
                  await api.resendVerification();
                  setResend("sent");
                } catch {
                  setResend("idle");
                }
              }}
            >
              {resend === "sent" ? "Email sent" : resend === "sending" ? "Sending…" : "Resend email"}
            </button>
          }
        >
          We sent a confirmation link to <strong>{user?.email}</strong>. Open it, then try publishing again.
        </Notice>
      ) : error ? (
        <Notice tone="danger">{error.message}</Notice>
      ) : unverified ? (
        <p className="muted small">You'll need to confirm your email address before publishing.</p>
      ) : null}
    </section>
  );
}

function DetailsSection({ page, setPage, saved }: EditorProps) {
  const [title, setTitle] = useState(page.title);
  const [bio, setBio] = useState(page.bio);
  const [busy, setBusy] = useState(false);
  const [ok, setOk] = useState(false);
  const [errors, setErrors] = useState<{ title?: string; bio?: string; general?: string; avatar?: string }>({});
  const dirty = title !== page.title || bio !== page.bio;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErrors({});
    setOk(false);
    try {
      const next = await api.updatePage(page.id, { title: title.trim(), bio });
      setPage(() => next);
      setTitle(next.title);
      setBio(next.bio);
      setOk(true);
      saved();
    } catch (err) {
      if (err instanceof ApiError && (err.field === "title" || err.field === "bio")) setErrors({ [err.field]: err.message });
      else setErrors({ general: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  }

  async function setAvatar(id: string | null) {
    setErrors({});
    try {
      const next = await api.updatePage(page.id, { avatar_upload_id: id });
      setPage((p) => ({ ...p, avatar_url: next.avatar_url, avatar_upload_id: next.avatar_upload_id, updated_at: next.updated_at }));
      saved();
    } catch (err) {
      setErrors({ avatar: errorMessage(err) });
    }
  }

  return (
    <section className="card section" aria-labelledby="details-heading">
      <h2 id="details-heading">Profile</h2>
      <ImageField
        label="Avatar"
        shape="circle"
        imageUrl={page.avatar_url}
        error={errors.avatar}
        onChange={(up) => setAvatar(up ? up.id : null)}
      />
      <form className="stack" onSubmit={onSubmit} noValidate>
        {errors.general && <Notice tone="danger">{errors.general}</Notice>}
        <TextField label="Title" value={title} onChange={(v) => { setTitle(v); setOk(false); }} required maxLength={120} error={errors.title} />
        <TextField
          label="Bio"
          value={bio}
          onChange={(v) => { setBio(v); setOk(false); }}
          multiline
          rows={3}
          maxLength={300}
          hint={`${bio.length}/300`}
          error={errors.bio}
        />
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={!dirty || busy || !title.trim()}>
            {busy ? <Spinner label="Saving" /> : null}
            Save profile
          </button>
          {ok && !dirty && <span className="save-ok" role="status">Saved</span>}
        </div>
      </form>
    </section>
  );
}

function AddressSection({ page, setPage, saved }: EditorProps) {
  const [slug, setSlug] = useState(page.slug);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [ok, setOk] = useState(false);
  const status = useSlugCheck(slug, page.slug);
  const changed = slug !== page.slug;
  const base = page.url.endsWith(page.slug) ? page.url.slice(0, -page.slug.length) : `${window.location.origin}/`;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!changed || status.kind !== "available") return;
    setBusy(true);
    setError(null);
    try {
      const next = await api.updatePage(page.id, { slug });
      setPage(() => next);
      setOk(true);
      saved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card section" aria-labelledby="address-heading">
      <h2 id="address-heading">Page address</h2>
      <form className="stack" onSubmit={onSubmit} noValidate>
        <TextField
          label="Address"
          value={slug}
          onChange={(v) => {
            setSlug(normalizeSlug(v));
            setOk(false);
            setError(null);
          }}
          prefix={base.replace(/^https?:\/\//, "")}
          required
          spellCheck={false}
          maxLength={32}
          error={error ?? slugStatusError(status)}
          status={<SlugStatusBadge status={status} />}
          hint={changed ? "Your old address will stop working. Update any links you've shared." : undefined}
        />
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={!changed || status.kind !== "available" || busy}>
            {busy ? <Spinner label="Saving" /> : null}
            Change address
          </button>
          {changed && (
            <button type="button" className="btn btn-ghost" onClick={() => setSlug(page.slug)}>
              Reset
            </button>
          )}
          {ok && !changed && <span className="save-ok" role="status">Address updated</span>}
        </div>
      </form>
    </section>
  );
}
