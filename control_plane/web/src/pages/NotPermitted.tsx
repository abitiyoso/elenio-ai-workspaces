import { Link } from "react-router-dom";
import { EmptyState } from "../components/ui";

export function NotPermitted({ area }: { area: string }) {
  return (
    <div className="card cardbody">
      <EmptyState title={`${area} is not available to you`} icon="lock">
        Your current roles do not include this administration area. Access is decided by the server; if you need it, ask a
        platform administrator. <Link to="/">Back to Home</Link>
      </EmptyState>
    </div>
  );
}

export function NotFoundPage() {
  return (
    <div className="card cardbody">
      <EmptyState title="Page not found" icon="info">
        <Link to="/">Back to Home</Link>
      </EmptyState>
    </div>
  );
}
