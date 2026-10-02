import { useState } from "react";
import { api, errorMessage, type LinkPreview } from "../../api";
import { Icon } from "../../components/Icon";
import { Spinner } from "../../components/ui";

interface LinkPreviewHelperProps {
  url: string;
  title: string;
  subtitle: string;
  thumbnailId: string;
  onUse: (key: "title" | "subtitle" | "thumbnail_upload_id", value: string) => void;
}

/**
 * Fetches the destination's title, description and image on request. The
 * title and description are offered as suggestions; the image is stored as
 * the user's own upload and applied when the link has no thumbnail yet.
 */
export function LinkPreviewHelper({ url, title, subtitle, thumbnailId, onUse }: LinkPreviewHelperProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<LinkPreview | null>(null);
  const [fetchedFor, setFetchedFor] = useState("");

  const target = url.trim();
  const usable = /^(https?:\/\/)?[^\s/:]+\.[^\s]+$/i.test(target);

  async function fetchPreview() {
    setBusy(true);
    setError(null);
    try {
      const p = await api.linkPreview(target);
      setPreview(p);
      setFetchedFor(target);
      if (p.thumbnail_upload_id && !thumbnailId) onUse("thumbnail_upload_id", p.thumbnail_upload_id);
    } catch (err) {
      setPreview(null);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  const stale = preview !== null && fetchedFor !== target;
  const showTitle = preview?.title && preview.title !== title.trim();
  const showSubtitle = preview?.description && preview.description !== subtitle.trim();
  const showImage = preview?.thumbnail_upload_id && preview.thumbnail_upload_id !== thumbnailId;

  return (
    <div className="link-preview-helper">
      <button type="button" className="btn btn-sm" disabled={!usable || busy} onClick={() => void fetchPreview()}>
        {busy ? <Spinner label="Fetching" /> : <Icon name="refresh" size={16} />}
        <span>{busy ? "Fetching…" : "Fetch title and image"}</span>
      </button>
      {error && <p className="field-error">{error}</p>}
      {preview && !stale && (
        <div className="link-preview-suggestions" aria-live="polite">
          {!preview.title && !preview.description && !preview.thumbnail_upload_id && (
            <p className="field-hint">That page doesn't share a title, description or image.</p>
          )}
          {showTitle && (
            <button type="button" className="btn btn-sm btn-ghost suggestion" onClick={() => onUse("title", preview.title)}>
              Use: <span className="suggestion-text">{preview.title}</span>
            </button>
          )}
          {showSubtitle && (
            <button type="button" className="btn btn-sm btn-ghost suggestion" onClick={() => onUse("subtitle", preview.description)}>
              Use as subtitle: <span className="suggestion-text">{preview.description}</span>
            </button>
          )}
          {showImage && preview.thumbnail_upload_id && (
            <button
              type="button"
              className="btn btn-sm btn-ghost suggestion"
              onClick={() => onUse("thumbnail_upload_id", preview.thumbnail_upload_id ?? "")}
            >
              {preview.thumbnail_url && <img className="suggestion-thumb" src={preview.thumbnail_url} alt="" />}
              <span>Use the page's image as thumbnail</span>
            </button>
          )}
          {preview.image_error && <p className="field-hint">{preview.image_error}</p>}
        </div>
      )}
    </div>
  );
}
