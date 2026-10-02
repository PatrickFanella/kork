import type { Page } from "../../api";

export interface EditorProps {
  page: Page;
  /** Apply a local update to the loaded page. */
  setPage: (update: (page: Page) => Page) => void;
  /** Call after every successful save so the preview reloads. */
  saved: () => void;
}
