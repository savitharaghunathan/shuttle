import { Link } from "react-router-dom";

/** Placeholder until Task 8 implements the create-run form. */
export default function NewRun() {
  return (
    <main>
      <header className="page-header">
        <h1>New run</h1>
        <Link to="/">Back to runs</Link>
      </header>
      <p className="muted">Create-run form arrives in a later task.</p>
    </main>
  );
}
