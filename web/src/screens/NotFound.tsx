import { Link } from "react-router-dom";
import { useDocumentTitle } from "../components/ui";

export function NotFound() {
  useDocumentTitle("Not found");
  return (
    <div className="container narrow empty-state">
      <h1>Page not found</h1>
      <p className="muted">That address doesn't match anything in the editor.</p>
      <Link className="btn btn-primary" to="/">
        Back to your pages
      </Link>
    </div>
  );
}
