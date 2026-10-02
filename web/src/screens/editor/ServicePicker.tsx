import { useId, useMemo, useState, type CSSProperties } from "react";
import type { CatalogService } from "../../api";
import { useCatalog } from "../../catalog";
import { Icon } from "../../components/Icon";
import { serviceHue } from "../../lib/blocks";
import { matchesQuery } from "../../lib/validate";

export function ServiceMonogram({ service, size = 36 }: { service: Pick<CatalogService, "key" | "name">; size?: number }) {
  const catalog = useCatalog();
  const hue = serviceHue(service.key);
  const style = { "--hue": hue, width: size, height: size, fontSize: size * 0.42 } as CSSProperties;
  const svg = catalog.services.find((s) => s.key === service.key)?.icon_svg;
  if (svg) {
    // The markup comes from the server's fixed icon set, never from user input.
    return <span className="monogram monogram-icon" style={style} aria-hidden="true" dangerouslySetInnerHTML={{ __html: svg }} />;
  }
  const letters = service.name.replace(/[^A-Za-z0-9]/g, "").slice(0, 2) || "?";
  return (
    <span className="monogram" style={style} aria-hidden="true">
      {letters.charAt(0).toUpperCase()}
      {letters.charAt(1).toLowerCase()}
    </span>
  );
}

function inputHint(s: CatalogService): string {
  if (s.copy_only) return "Copy button";
  switch (s.input) {
    case "handle":
      return `${s.prefix}${s.placeholder || "username"}`;
    case "url":
      return "Profile link";
    case "email":
      return "Email";
    case "phone":
      return "Phone number";
    default:
      return s.placeholder || "Text";
  }
}

export function ServicePicker({ onPick, onUseLink }: { onPick: (service: CatalogService) => void; onUseLink: () => void }) {
  const catalog = useCatalog();
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<string>("all");
  const searchId = useId();
  const resultsId = useId();

  const categories = useMemo(() => {
    const known = catalog.categories.map((c) => c.key);
    const extra = [...new Set(catalog.services.map((s) => s.category))].filter((k) => !known.includes(k));
    return [...catalog.categories, ...extra.map((k) => ({ key: k, name: k.charAt(0).toUpperCase() + k.slice(1) }))];
  }, [catalog]);

  const matches = useMemo(
    () =>
      catalog.services.filter(
        (s) => (category === "all" || s.category === category) && matchesQuery(query, s.name, s.key, s.category),
      ),
    [catalog.services, category, query],
  );

  const counts = useMemo(() => {
    const c = new Map<string, number>();
    for (const s of catalog.services) {
      if (matchesQuery(query, s.name, s.key, s.category)) c.set(s.category, (c.get(s.category) ?? 0) + 1);
    }
    return c;
  }, [catalog.services, query]);

  const groups = categories
    .map((c) => ({ ...c, services: matches.filter((s) => s.category === c.key) }))
    .filter((g) => g.services.length > 0);

  const totalForQuery = [...counts.values()].reduce((a, b) => a + b, 0);

  return (
    <div className="service-picker">
      <div className="service-search">
        <label htmlFor={searchId} className="visually-hidden">
          Search services
        </label>
        <Icon name="search" />
        <input
          id={searchId}
          type="search"
          value={query}
          autoFocus
          autoComplete="off"
          spellCheck={false}
          placeholder={`Search ${catalog.services.length} services: Instagram, Venmo, Spotify…`}
          aria-controls={resultsId}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              const first = matches[0];
              if (first) onPick(first);
            }
          }}
        />
      </div>

      <div className="chips" role="group" aria-label="Filter by category">
        <button type="button" className="chip" aria-pressed={category === "all"} onClick={() => setCategory("all")}>
          All <span className="chip-count">{totalForQuery}</span>
        </button>
        {categories.map((c) => (
          <button
            key={c.key}
            type="button"
            className="chip"
            aria-pressed={category === c.key}
            disabled={!counts.get(c.key)}
            onClick={() => setCategory(c.key)}
          >
            {c.name} <span className="chip-count">{counts.get(c.key) ?? 0}</span>
          </button>
        ))}
      </div>

      <div id={resultsId} className="service-results" aria-live="polite">
        {groups.length === 0 ? (
          <div className="empty-inline">
            <p>
              No services match <strong>“{query}”</strong>
              {category !== "all" ? " in this category" : ""}.
            </p>
            <div className="row">
              {category !== "all" && (
                <button type="button" className="btn btn-sm" onClick={() => setCategory("all")}>
                  Search all categories
                </button>
              )}
              <button type="button" className="btn btn-sm" onClick={onUseLink}>
                <Icon name="link" />
                <span>Add a regular link instead</span>
              </button>
            </div>
          </div>
        ) : (
          groups.map((g) => (
            <section key={g.key} className="service-group" aria-labelledby={`${resultsId}-${g.key}`}>
              <h4 id={`${resultsId}-${g.key}`}>{g.name}</h4>
              <ul className="service-grid">
                {g.services.map((s) => (
                  <li key={s.key}>
                    <button type="button" className="service-tile" onClick={() => onPick(s)}>
                      <ServiceMonogram service={s} />
                      <span className="service-tile-text">
                        <span className="service-tile-name">{s.name}</span>
                        <span className="service-tile-hint">{inputHint(s)}</span>
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ))
        )}
      </div>
      {query && matches[0] && (
        <p className="muted small">
          Press Enter to add <strong>{matches[0].name}</strong>.
        </p>
      )}
    </div>
  );
}
