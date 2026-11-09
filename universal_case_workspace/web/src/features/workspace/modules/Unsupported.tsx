export function UnsupportedView({ view, label }: { view: string; label: string }) {
  return (
    <div className="card pad unsupported" role="note">
      <h2>{label}</h2>
      <p className="notice warning">This view uses the kind <code>{view}</code>, which this version of the Case Workspace does not support. The pack may need a newer workspace release.</p>
    </div>
  );
}
