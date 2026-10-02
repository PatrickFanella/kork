import type { Domain, DomainMode, DomainRecord } from "../api";

/** Records to show for a domain, falling back to the older txt/target fields. */
export function domainRecords(domain: Domain): DomainRecord[] {
  if (domain.records) return domain.records;
  return [
    { type: "TXT", name: domain.txt_name, value: domain.txt_value, purpose: "ownership" },
    { type: "CNAME", name: domain.hostname, value: domain.target, purpose: "routing" },
  ];
}

/** Badge text and class for a domain's status. */
export function domainBadge(domain: Domain, mode: DomainMode): { label: string; className: string } {
  const status = domain.status ?? (domain.verified ? "active" : "pending");
  if (status === "active") return { label: "Verified", className: "badge-live" };
  if (status === "error") return { label: "Needs attention", className: "badge-danger" };
  return { label: mode === "cloudflare" ? "Pending" : "Waiting for DNS", className: "badge-draft" };
}
