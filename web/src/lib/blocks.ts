// Block form schemas: which fields each block type edits, and how to clean
// the values before sending them to the API.

import type { BlockData, BlockType, CatalogService, ServiceInput } from "../api";
import { stripPrefix } from "./validate";

export type FieldKind = "text" | "textarea" | "url" | "email" | "tel" | "checkbox" | "image";

export interface FieldSpec {
  key: string;
  label: string;
  kind: FieldKind;
  required?: boolean;
  placeholder?: string;
  hint?: string;
  maxLength?: number;
}

export const BLOCK_ORDER: BlockType[] = [
  "service",
  "link",
  "heading",
  "text",
  "image",
  "embed",
  "copy",
  "contact",
  "map",
  "qr",
  "divider",
];

/** Local fallbacks used when the catalog omits a block type. */
export const BLOCK_FALLBACK: Record<BlockType, { name: string; description: string }> = {
  service: { name: "Social / payment / contact", description: "Link a profile, payment handle or contact method." },
  link: { name: "Link", description: "A button that opens any web address." },
  heading: { name: "Heading", description: "A section title to group your links." },
  text: { name: "Text", description: "A short paragraph. Links are detected automatically." },
  divider: { name: "Divider", description: "A line to separate sections." },
  image: { name: "Image", description: "A picture, optionally linking somewhere." },
  embed: { name: "Embed", description: "A video or music player from a supported site." },
  qr: { name: "QR code", description: "Show a scannable code for any text or link." },
  copy: { name: "Copy button", description: "A button that copies text, like a code or address." },
  contact: { name: "Contact card", description: "A 'Save contact' button that downloads a vCard." },
  map: { name: "Map", description: "Opens an address in the visitor's maps app." },
};

const SHOW_QR: FieldSpec = {
  key: "show_qr",
  label: "Show a QR code next to this button",
  kind: "checkbox",
};

export function blockFields(type: BlockType, service?: CatalogService): FieldSpec[] {
  switch (type) {
    case "link":
      return [
        { key: "title", label: "Button text", kind: "text", required: true, maxLength: 120, placeholder: "My latest project" },
        { key: "url", label: "Web address", kind: "url", required: true, placeholder: "https://", hint: "http, https, mailto, tel and sms links work." },
        { key: "subtitle", label: "Subtitle", kind: "text", maxLength: 160, placeholder: "Optional second line" },
        { key: "thumbnail_upload_id", label: "Thumbnail", kind: "image" },
        SHOW_QR,
      ];
    case "service":
      return [
        {
          key: "value",
          label: serviceValueLabel(service?.input ?? "text"),
          kind: serviceInputKind(service?.input ?? "text"),
          required: true,
          placeholder: service?.placeholder,
        },
        { key: "label", label: "Button text", kind: "text", maxLength: 120, placeholder: service?.name, hint: "Leave empty to use the service name." },
        ...(service?.copy_only ? [] : [SHOW_QR]),
      ];
    case "heading":
      return [{ key: "text", label: "Heading", kind: "text", required: true, maxLength: 120 }];
    case "text":
      return [{ key: "text", label: "Text", kind: "textarea", required: true, maxLength: 2000 }];
    case "divider":
      return [];
    case "image":
      return [
        { key: "upload_id", label: "Image", kind: "image", required: true },
        { key: "alt", label: "Description for screen readers", kind: "text", required: true, maxLength: 300, placeholder: "What's in the picture?" },
        { key: "url", label: "Link when clicked", kind: "url", placeholder: "https:// (optional)" },
      ];
    case "embed":
      return [{ key: "url", label: "Video or track address", kind: "url", required: true, placeholder: "https://" }];
    case "qr":
      return [
        { key: "value", label: "Text or link to encode", kind: "text", required: true },
        { key: "label", label: "Caption", kind: "text", maxLength: 120 },
      ];
    case "copy":
      return [
        { key: "label", label: "Button text", kind: "text", required: true, maxLength: 120, placeholder: "Copy discount code" },
        { key: "value", label: "Text to copy", kind: "text", required: true },
      ];
    case "contact":
      return [
        { key: "name", label: "Name", kind: "text", required: true, maxLength: 120 },
        { key: "phone", label: "Phone", kind: "tel" },
        { key: "email", label: "Email", kind: "email" },
        { key: "org", label: "Organization", kind: "text" },
        { key: "url", label: "Website", kind: "url", placeholder: "https://" },
        { key: "note", label: "Note", kind: "textarea", maxLength: 500 },
      ];
    case "map":
      return [
        { key: "address", label: "Address", kind: "text", required: true },
        { key: "label", label: "Button text", kind: "text", maxLength: 120, placeholder: "Find us" },
      ];
  }
}

export function serviceInputKind(input: ServiceInput): FieldKind {
  switch (input) {
    case "url":
      return "url";
    case "email":
      return "email";
    case "phone":
      return "tel";
    default:
      return "text";
  }
}

export function serviceValueLabel(input: ServiceInput): string {
  switch (input) {
    case "handle":
      return "Username";
    case "url":
      return "Profile address";
    case "email":
      return "Email address";
    case "phone":
      return "Phone number";
    default:
      return "Value";
  }
}

/**
 * Trim strings, drop empty optional fields, strip a typed service prefix, and
 * keep booleans only when true. Unknown keys are preserved.
 */
export function cleanBlockData(
  type: BlockType,
  data: BlockData,
  service?: CatalogService,
): BlockData {
  const out: BlockData = {};
  for (const [key, raw] of Object.entries(data)) {
    if (typeof raw === "boolean") {
      if (raw) out[key] = true;
      continue;
    }
    if (typeof raw !== "string") continue;
    let v = raw.trim();
    if (type === "service" && key === "value" && service?.input === "handle") {
      v = stripPrefix(v, service.prefix);
    }
    if (v !== "") out[key] = type === "text" || key === "note" ? raw.replace(/\s+$/, "") : v;
  }
  if (type === "service" && service) out.service = service.key;
  return out;
}

/** Required fields that are empty, mapped to a message. */
export function missingRequired(fields: FieldSpec[], data: BlockData): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const f of fields) {
    if (!f.required) continue;
    const v = data[f.key];
    if (v === undefined || v === false || (typeof v === "string" && v.trim() === "")) {
      errors[f.key] = f.kind === "image" ? "Upload an image." : "This field is required.";
    }
  }
  return errors;
}

/** One-line secondary text for the block list (service blocks: the prefixed value). */
export function blockSummary(type: BlockType, data: BlockData, service?: CatalogService): string {
  const s = (k: string) => (typeof data[k] === "string" ? (data[k] as string) : "");
  switch (type) {
    case "service": {
      const value = s("value");
      const shown = service?.input === "handle" && value ? `${service.prefix}${value}` : value;
      return shown;
    }
    case "link":
    case "embed":
      return s("url");
    case "image":
      return s("alt");
    case "qr":
    case "copy":
      return s("value");
    case "contact":
      return [s("phone"), s("email")].filter(Boolean).join(" · ");
    case "map":
      return s("address");
    case "text":
      return s("text").split("\n")[0] ?? "";
    default:
      return "";
  }
}

/** A stable hue for a service monogram, derived from its key. */
export function serviceHue(key: string): number {
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) % 360;
  return h;
}
