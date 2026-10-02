import { useState, type FormEvent } from "react";
import { ApiError, api, errorMessage, type Domain, type DomainMode, type DomainRecord } from "../../api";
import { Icon } from "../../components/Icon";
import { ConfirmButton, CopyRow, Notice, Spinner, TextField } from "../../components/ui";
import { hostnameProblem, normalizeHostname } from "../../lib/validate";
import type { EditorProps } from "./types";
import { ProBadge, UpgradeNote } from "../../components/Pro";
import { planAllows, usePlan } from "../../plan";
import { useCatalog } from "../../catalog";
import { domainBadge, domainRecords } from "../../lib/domains";

export function DomainsTab({ page, setPage, saved }: EditorProps) {
  const [hostname, setHostname] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { plan } = usePlan();
  const catalog = useCatalog();
  const mode: DomainMode = catalog.domain_mode ?? "dns";
  const allowed = planAllows(plan, "custom_domains");
  const normalized = normalizeHostname(hostname);
  const problem = hostname ? hostnameProblem(normalized) : null;

  const setDomains = (update: (d: Domain[]) => Domain[]) => setPage((p) => ({ ...p, domains: update(p.domains ?? []) }));

  async function onAdd(e: FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (hostnameProblem(normalized)) return;
    setBusy(true);
    setError(null);
    try {
      const d = await api.addDomain(page.id, normalized);
      setDomains((ds) => [...ds, d]);
      setHostname("");
      setTouched(false);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  const domains = page.domains ?? [];

  return (
    <div className="stack-lg">
      <section className="card section" aria-labelledby="domain-add-heading">
        <h2 id="domain-add-heading">
          Custom domain {!allowed && <ProBadge />}
        </h2>
        <p className="muted">
          Show this page on your own domain, like links.yourname.com. HTTPS is set up automatically
          {mode === "cloudflare" ? " once the domain points at us." : "."}
        </p>
        {!allowed && <UpgradeNote>Custom domains are part of Pro.</UpgradeNote>}
        {plan?.domains_grace_until && (
          <Notice tone="warning" title="Pro has ended">
            Your custom domain keeps working until {new Date(plan.domains_grace_until).toLocaleDateString()}. After that it stays
            attached but stops showing this page until you have Pro again.
          </Notice>
        )}
        <form className="inline-form" onSubmit={onAdd} noValidate>
          <TextField
            label="Hostname"
            value={hostname}
            onChange={(v) => {
              setHostname(v);
              setError(null);
            }}
            placeholder="links.example.com"
            required
            spellCheck={false}
            inputMode="url"
            autoComplete="off"
            disabled={!allowed}
            error={error ?? (touched ? problem : null)}
          />
          <button type="submit" className="btn btn-primary" disabled={busy || !hostname || !allowed}>
            {busy ? <Spinner label="Adding" /> : <Icon name="plus" />}
            <span>Add domain</span>
          </button>
        </form>
      </section>

      {domains.map((d) => (
        <DomainCard
          key={d.id}
          pageId={page.id}
          domain={d}
          mode={d.mode ?? mode}
          onChange={(next) => {
            setDomains((ds) => ds.map((x) => (x.id === next.id ? next : x)));
            saved();
          }}
          onDelete={() => setDomains((ds) => ds.filter((x) => x.id !== d.id))}
        />
      ))}
    </div>
  );
}

function DomainCard({
  pageId,
  domain,
  mode,
  onChange,
  onDelete,
}: {
  pageId: string;
  domain: Domain;
  mode: DomainMode;
  onChange: (d: Domain) => void;
  onDelete: () => void;
}) {
  const [verifying, setVerifying] = useState(false);
  const [checked, setChecked] = useState(false);
  const [reason, setReason] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const apex = domain.hostname.split(".").length === 2;
  const records = domainRecords(domain);
  const routing = records.filter((r) => r.purpose === "routing");
  const ownership = records.filter((r) => r.purpose === "ownership");
  const certificate = records.filter((r) => r.purpose === "certificate");
  const problems = domain.errors ?? [];
  const badge = domainBadge(domain, mode);

  async function verify() {
    setVerifying(true);
    setReason(null);
    setError(null);
    try {
      const d = await api.verifyDomain(pageId, domain.id);
      onChange(d);
      setChecked(true);
      if (!d.verified && mode === "dns") setReason("DNS records weren't found yet.");
    } catch (err) {
      if (err instanceof ApiError && err.status === 422) setReason(err.message);
      else setError(errorMessage(err));
    } finally {
      setVerifying(false);
    }
  }

  const routingStep = (
    <li>
      <p>
        {apex ? (
          mode === "cloudflare" ? (
            <>
              Point <strong>{domain.hostname}</strong> at the target below with a <strong>CNAME</strong>. For a root
              domain your DNS provider must support CNAME flattening or an ALIAS/ANAME record; otherwise use a subdomain
              such as links.{domain.hostname}.
            </>
          ) : (
            <>
              Point <strong>{domain.hostname}</strong> at our server. For a root domain, most providers need{" "}
              <strong>A/AAAA</strong> records for the address of the target below (or an ALIAS/ANAME record if your
              provider supports it). For a subdomain, use a <strong>CNAME</strong>.
            </>
          )
        ) : (
          <>
            Add a <strong>CNAME</strong> record for <strong>{domain.hostname}</strong> pointing to:
          </>
        )}
      </p>
      {routing.map((r) => (
        <CopyRow key={r.name + r.value} label="Target" value={r.value} />
      ))}
    </li>
  );

  const recordStep = (list: DomainRecord[], intro: string) =>
    list.length > 0 && (
      <li>
        <p>{intro}</p>
        {list.map((r) => (
          <div key={r.type + r.name + r.value} className="dns-record">
            <CopyRow label={`${r.type} name`} value={r.name} />
            <CopyRow label="Value" value={r.value} />
          </div>
        ))}
      </li>
    );

  const verifyButton = (
    <button type="button" className="btn btn-primary" onClick={verify} disabled={verifying}>
      {verifying ? <Spinner label={mode === "cloudflare" ? "Checking" : "Checking DNS"} /> : <Icon name="refresh" />}
      <span>{verifying ? "Checking…" : mode === "cloudflare" ? "Check now" : "Verify"}</span>
    </button>
  );

  return (
    <section className="card section domain-card" aria-label={domain.hostname}>
      <div className="section-row">
        <div className="domain-title">
          <h3>{domain.hostname}</h3>
          <span className={`badge ${badge.className}`}>{badge.label}</span>
          {domain.active === false && <span className="badge badge-danger">Inactive: needs Pro</span>}
        </div>
        <ConfirmButton
          label="Remove"
          confirmLabel="Remove domain"
          busyLabel="Removing"
          onConfirm={async () => {
            try {
              await api.deleteDomain(pageId, domain.id);
              onDelete();
            } catch (err) {
              setError(errorMessage(err));
            }
          }}
        />
      </div>

      {domain.verified && domain.active === false ? (
        <UpgradeNote>
          This domain is kept, but {domain.hostname} doesn't show your page while your account is on Free.
        </UpgradeNote>
      ) : domain.verified ? (
        <p className="muted">
          This page is live at{" "}
          <a href={`https://${domain.hostname}/`} target="_blank" rel="noreferrer">
            https://{domain.hostname}
          </a>
          .
          {mode === "dns" && " The certificate is issued on the first visit, so the first load can take a few seconds."}
        </p>
      ) : mode === "cloudflare" ? (
        <ol className="dns-steps">
          {routingStep}
          {recordStep(ownership, "Add this TXT record so Cloudflare can confirm you own the domain:")}
          {recordStep(certificate, "Add these records so the HTTPS certificate can be issued:")}
          <li>
            <p>
              We check with Cloudflare every 10 minutes and mark the domain verified once the domain and its certificate
              are active. DNS changes can take up to an hour.
              {domain.checked_at && ` Last checked ${new Date(domain.checked_at).toLocaleString()}.`}
            </p>
            {verifyButton}
          </li>
        </ol>
      ) : (
        <ol className="dns-steps">
          {recordStep(ownership, "At your DNS provider, add a TXT record:")}
          {routingStep}
          <li>
            <p>Wait a few minutes for DNS to update, then verify.</p>
            {verifyButton}
          </li>
        </ol>
      )}
      {!domain.verified && problems.length > 0 && (
        <Notice tone={domain.status === "error" ? "danger" : "warning"} title={domain.status === "error" ? "Can't verify" : "Not active yet"}>
          <ul className="domain-errors">
            {problems.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </Notice>
      )}
      {checked && mode === "cloudflare" && !domain.verified && problems.length === 0 && (
        <Notice tone="info">Cloudflare is still issuing the certificate. This usually takes a few minutes.</Notice>
      )}
      {reason && (
        <Notice tone="warning" title="Not verified yet">
          {reason} DNS changes can take up to an hour; try again later.
        </Notice>
      )}
      {error && <Notice tone="danger">{error}</Notice>}
    </section>
  );
}
