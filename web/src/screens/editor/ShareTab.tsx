import { useState } from "react";
import { api } from "../../api";
import { Icon } from "../../components/Icon";
import { CopyButton, Notice } from "../../components/ui";
import type { EditorProps } from "./types";

export function ShareTab({ page }: EditorProps) {
  const [qrFailed, setQrFailed] = useState(false);
  const qr = `${api.qrUrl(page.id)}?v=${encodeURIComponent(page.slug)}`;
  const verified = (page.domains ?? []).filter((d) => d.verified);

  return (
    <div className="stack-lg">
      {!page.published && (
        <Notice tone="info" title="This page is a draft">
          Visitors will see it once you publish it from the Profile tab.
        </Notice>
      )}

      <section className="card section" aria-labelledby="link-heading">
        <h2 id="link-heading">Your link</h2>
        <div className="share-url">
          <code>{page.url}</code>
          <CopyButton text={page.url} label="Copy link" className="btn btn-primary btn-sm" />
          {page.published && (
            <a className="btn btn-sm btn-ghost" href={page.url} target="_blank" rel="noreferrer">
              <Icon name="external" />
              <span>Open</span>
            </a>
          )}
        </div>
        {verified.map((d) => (
          <div className="share-url" key={d.id}>
            <code>https://{d.hostname}</code>
            <CopyButton text={`https://${d.hostname}`} label="Copy" />
          </div>
        ))}
      </section>

      <section className="card section" aria-labelledby="qr-heading">
        <h2 id="qr-heading">QR code</h2>
        <p className="muted">For posters, business cards and slides. It always points to {page.url.replace(/^https?:\/\//, "")}.</p>
        <div className="qr-block">
          <div className="qr-frame">
            {qrFailed ? (
              <span className="muted small">QR code unavailable</span>
            ) : (
              <img src={qr} alt={`QR code linking to ${page.url}`} width={180} height={180} onError={() => setQrFailed(true)} />
            )}
          </div>
          <a className="btn" href={qr} download={`${page.slug}-qr.svg`}>
            <Icon name="down" />
            <span>Download SVG</span>
          </a>
        </div>
      </section>
    </div>
  );
}
