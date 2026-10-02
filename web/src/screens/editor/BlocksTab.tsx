import { useMemo, useRef, useState, type DragEvent } from "react";
import { api, errorMessage, type Block, type BlockType, type CatalogService } from "../../api";
import { useCatalog } from "../../catalog";
import { BLOCK_ICONS, Icon } from "../../components/Icon";
import { ConfirmButton, Notice, Switch } from "../../components/ui";
import { BLOCK_FALLBACK, BLOCK_ORDER, blockSummary } from "../../lib/blocks";
import { MAX_BLOCKS, moveItem } from "../../lib/validate";
import { BlockForm } from "./BlockForm";
import { ImportPanel } from "./ImportPanel";
import { ServiceMonogram, ServicePicker } from "./ServicePicker";
import type { EditorProps } from "./types";

type AddState =
  | null
  | { step: "type" }
  | { step: "service" }
  | { step: "form"; type: BlockType; service?: CatalogService };

export function BlocksTab({ page, setPage, saved }: EditorProps) {
  const catalog = useCatalog();
  const [adding, setAdding] = useState<AddState>(null);
  const [importing, setImporting] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [announce, setAnnounce] = useState("");
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropIndex, setDropIndex] = useState<number | null>(null);
  const orderQueue = useRef<Promise<void>>(Promise.resolve());

  const services = useMemo(() => new Map(catalog.services.map((s) => [s.key, s])), [catalog.services]);
  const typeName = (t: BlockType) =>
    catalog.block_types.find((b) => b.key === t)?.name ?? BLOCK_FALLBACK[t]?.name ?? t;

  const blocks = page.blocks;
  const atLimit = blocks.length >= MAX_BLOCKS;

  function setBlocks(update: (b: Block[]) => Block[]) {
    setPage((p) => ({ ...p, blocks: update(p.blocks) }));
  }

  function commitOrder(next: Block[], moved: Block) {
    const prev = blocks;
    setBlocks(() => next.map((b, i) => ({ ...b, position: i })));
    const pos = next.findIndex((b) => b.id === moved.id) + 1;
    setAnnounce(`Moved ${moved.label || typeName(moved.type)} to position ${pos} of ${next.length}.`);
    const ids = next.map((b) => b.id);
    // Serialize order updates so rapid moves arrive in order.
    orderQueue.current = orderQueue.current.then(async () => {
      try {
        await api.reorderBlocks(page.id, ids);
        saved();
      } catch (err) {
        setError(`Couldn't save the new order: ${errorMessage(err)}`);
        setBlocks(() => prev);
      }
    });
  }

  function move(index: number, delta: -1 | 1) {
    const target = index + delta;
    const block = blocks[index];
    if (!block || target < 0 || target >= blocks.length) return;
    commitOrder(moveItem(blocks, index, delta), block);
  }

  async function toggle(block: Block, enabled: boolean) {
    setError(null);
    setBlocks((bs) => bs.map((b) => (b.id === block.id ? { ...b, enabled } : b)));
    try {
      const updated = await api.updateBlock(page.id, block.id, { enabled });
      setBlocks((bs) => bs.map((b) => (b.id === block.id ? updated : b)));
      saved();
    } catch (err) {
      setBlocks((bs) => bs.map((b) => (b.id === block.id ? { ...b, enabled: !enabled } : b)));
      setError(errorMessage(err));
    }
  }

  async function remove(block: Block) {
    setError(null);
    try {
      await api.deleteBlock(page.id, block.id);
      setBlocks((bs) => bs.filter((b) => b.id !== block.id));
      setAnnounce(`Deleted ${block.label || typeName(block.type)}.`);
      if (editingId === block.id) setEditingId(null);
      saved();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  // Native drag and drop for pointer users; the arrow buttons cover keyboard and touch.
  function onDragStart(e: DragEvent, id: string) {
    setDragId(id);
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", id);
  }
  function onDragOver(e: DragEvent<HTMLLIElement>, index: number) {
    if (!dragId) return;
    e.preventDefault();
    const rect = e.currentTarget.getBoundingClientRect();
    const after = e.clientY > rect.top + rect.height / 2;
    setDropIndex(after ? index + 1 : index);
  }
  function onDrop(e: DragEvent) {
    e.preventDefault();
    const from = blocks.findIndex((b) => b.id === dragId);
    const block = blocks[from];
    if (block && dropIndex !== null) {
      const to = dropIndex > from ? dropIndex - 1 : dropIndex;
      if (to !== from) {
        const next = blocks.slice();
        next.splice(from, 1);
        next.splice(to, 0, block);
        commitOrder(next, block);
      }
    }
    setDragId(null);
    setDropIndex(null);
  }

  return (
    <div className="blocks-tab">
      <div className="visually-hidden" aria-live="polite">
        {announce}
      </div>
      {error && (
        <Notice
          tone="danger"
          action={
            <button type="button" className="btn btn-sm btn-ghost" onClick={() => setError(null)}>
              Dismiss
            </button>
          }
        >
          {error}
        </Notice>
      )}

      {importing && (
        <ImportPanel page={page} setPage={setPage} saved={saved} announce={setAnnounce} onClose={() => setImporting(false)} />
      )}

      {adding ? (
        <AddBlockPanel
          state={adding}
          setState={setAdding}
          typeName={typeName}
          onCreate={async (type, data) => {
            const block = await api.createBlock(page.id, type, data, true);
            setBlocks((bs) => [...bs, block]);
            setAdding(null);
            setAnnounce(`Added ${block.label || typeName(block.type)}.`);
            saved();
          }}
        />
      ) : (
        <div className="blocks-toolbar">
          <div className="blocks-toolbar-actions">
            <button type="button" className="btn btn-primary" onClick={() => setAdding({ step: "type" })} disabled={atLimit}>
              <Icon name="plus" />
              <span>Add block</span>
            </button>
            {!importing && (
              <button type="button" className="btn" onClick={() => setImporting(true)} disabled={atLimit}>
                <Icon name="upload" />
                <span>Import</span>
              </button>
            )}
          </div>
          <span className="muted small">
            {blocks.length} {blocks.length === 1 ? "block" : "blocks"}
            {atLimit ? ` (limit ${MAX_BLOCKS})` : ""}
          </span>
        </div>
      )}

      {blocks.length === 0 && !adding && !importing ? (
        <div className="empty-state">
          <Icon name="blocks" size={32} />
          <h2>Your page is empty</h2>
          <p className="muted">Add your social profiles, links, payment handles and more.</p>
          <button type="button" className="btn btn-primary" onClick={() => setAdding({ step: "service" })}>
            Add your first profile
          </button>
          <button type="button" className="btn" onClick={() => setImporting(true)}>
            Import from Linktree or another link page
          </button>
        </div>
      ) : (
        <ol className="block-list" onDragLeave={(e) => e.currentTarget === e.target && setDropIndex(null)}>
          {blocks.map((block, i) => {
            const service = block.type === "service" ? services.get(String(block.data.service ?? "")) : undefined;
            const name = block.label || typeName(block.type);
            const summary = blockSummary(block.type, block.data, service);
            const editing = editingId === block.id;
            return (
              <li
                key={block.id}
                className={[
                  "block-item",
                  block.enabled ? "" : "is-disabled",
                  editing ? "is-editing" : "",
                  dragId === block.id ? "is-dragging" : "",
                  dropIndex === i ? "drop-before" : "",
                  dropIndex === i + 1 && i === blocks.length - 1 ? "drop-after" : "",
                ]
                  .filter(Boolean)
                  .join(" ")}
                onDragOver={(e) => onDragOver(e, i)}
                onDrop={onDrop}
              >
                <div className="block-row">
                  <span
                    className="drag-handle"
                    draggable
                    onDragStart={(e) => onDragStart(e, block.id)}
                    onDragEnd={() => {
                      setDragId(null);
                      setDropIndex(null);
                    }}
                    title="Drag to reorder"
                    aria-hidden="true"
                  >
                    <svg width="12" height="18" viewBox="0 0 12 18" fill="currentColor">
                      <circle cx="3" cy="3" r="1.5" />
                      <circle cx="9" cy="3" r="1.5" />
                      <circle cx="3" cy="9" r="1.5" />
                      <circle cx="9" cy="9" r="1.5" />
                      <circle cx="3" cy="15" r="1.5" />
                      <circle cx="9" cy="15" r="1.5" />
                    </svg>
                  </span>
                  <span className="block-icon">
                    {service ? <ServiceMonogram service={service} size={32} /> : <Icon name={BLOCK_ICONS[block.type] ?? "blocks"} />}
                  </span>
                  <div className="block-text">
                    <span className="block-label">{name}</span>
                    <span className="block-meta">
                      <span className="block-type">{service ? service.name : typeName(block.type)}</span>
                      {summary && <span className="block-summary">{summary}</span>}
                    </span>
                  </div>
                  <div className="block-actions">
                    <Switch checked={block.enabled} onChange={(v) => void toggle(block, v)} label={`Show ${name} on page`} hideLabel />
                    <div className="move-buttons">
                      <button
                        type="button"
                        className="icon-btn"
                        aria-disabled={i === 0}
                        onClick={() => move(i, -1)}
                        title="Move up"
                      >
                        <Icon name="up" size={16} />
                        <span className="visually-hidden">Move {name} up</span>
                      </button>
                      <button
                        type="button"
                        className="icon-btn"
                        aria-disabled={i === blocks.length - 1}
                        onClick={() => move(i, 1)}
                        title="Move down"
                      >
                        <Icon name="down" size={16} />
                        <span className="visually-hidden">Move {name} down</span>
                      </button>
                    </div>
                    <button
                      type="button"
                      className="icon-btn"
                      aria-expanded={editing}
                      onClick={() => setEditingId(editing ? null : block.id)}
                      title="Edit"
                    >
                      <Icon name="edit" size={16} />
                      <span className="visually-hidden">Edit {name}</span>
                    </button>
                    <ConfirmButton label={`Delete ${name}`} confirmLabel="Delete" className="icon-btn icon-btn-danger" hideLabel onConfirm={() => remove(block)} />
                  </div>
                </div>
                {editing && (
                  <div className="block-edit">
                    <BlockForm
                      type={block.type}
                      service={service}
                      initial={block.data}
                      submitLabel="Save changes"
                      onCancel={() => setEditingId(null)}
                      onSubmit={async (data) => {
                        const updated = await api.updateBlock(page.id, block.id, { data });
                        setBlocks((bs) => bs.map((b) => (b.id === block.id ? updated : b)));
                        setEditingId(null);
                        saved();
                      }}
                    />
                  </div>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}

function AddBlockPanel({
  state,
  setState,
  typeName,
  onCreate,
}: {
  state: NonNullable<AddState>;
  setState: (s: AddState) => void;
  typeName: (t: BlockType) => string;
  onCreate: (type: BlockType, data: Record<string, string | boolean | undefined>) => Promise<void>;
}) {
  const catalog = useCatalog();
  const known = new Set<string>(BLOCK_ORDER);
  const fromCatalog = catalog.block_types.map((b) => b.key).filter((k): k is BlockType => known.has(k));
  const types: BlockType[] = fromCatalog.length ? fromCatalog : BLOCK_ORDER;
  const description = (t: BlockType) => catalog.block_types.find((b) => b.key === t)?.description || BLOCK_FALLBACK[t].description;

  const title =
    state.step === "type"
      ? "Add a block"
      : state.step === "service"
        ? "Choose a service"
        : state.type === "service" && state.service
          ? `Add ${state.service.name}`
          : `Add ${typeName(state.type).toLowerCase()}`;

  const back = () => {
    if (state.step === "form" && state.type === "service") setState({ step: "service" });
    else if (state.step === "type") setState(null);
    else setState({ step: "type" });
  };

  return (
    <section className="card add-panel" aria-labelledby="add-panel-title">
      <div className="add-panel-header">
        <button type="button" className="icon-btn" onClick={back} title="Back">
          <Icon name="back" size={16} />
          <span className="visually-hidden">Back</span>
        </button>
        <h2 id="add-panel-title">{title}</h2>
        <button type="button" className="icon-btn" onClick={() => setState(null)} title="Close">
          <Icon name="close" size={16} />
          <span className="visually-hidden">Close</span>
        </button>
      </div>

      {state.step === "type" && (
        <ul className="type-grid">
          {types.map((t) => (
            <li key={t} className={t === "service" ? "type-featured" : undefined}>
              <button
                type="button"
                className="type-card"
                autoFocus={t === types[0]}
                onClick={() => setState(t === "service" ? { step: "service" } : { step: "form", type: t })}
              >
                <span className="type-card-icon">
                  <Icon name={BLOCK_ICONS[t] ?? "blocks"} size={t === "service" ? 22 : 18} />
                </span>
                <span className="type-card-text">
                  <span className="type-card-name">{typeName(t)}</span>
                  <span className="type-card-desc">{description(t)}</span>
                </span>
                {t === "service" && (
                  <span className="type-card-preview" aria-hidden="true">
                    {catalog.services.slice(0, 5).map((s) => (
                      <ServiceMonogram key={s.key} service={s} size={26} />
                    ))}
                  </span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}

      {state.step === "service" && (
        <ServicePicker
          onPick={(service) => setState({ step: "form", type: "service", service })}
          onUseLink={() => setState({ step: "form", type: "link" })}
        />
      )}

      {state.step === "form" && (
        <BlockForm
          key={`${state.type}-${state.service?.key ?? ""}`}
          type={state.type}
          service={state.service}
          submitLabel="Add to page"
          onCancel={() => setState(null)}
          onChangeService={state.type === "service" ? () => setState({ step: "service" }) : undefined}
          onSubmit={(data) => onCreate(state.type, data)}
        />
      )}
    </section>
  );
}
