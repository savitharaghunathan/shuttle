import type { Stage } from "../api/types";

type Props = {
  stages: Stage[];
  selectedId: string | null;
  onSelect: (stageId: string) => void;
};

export default function StageList({ stages, selectedId, onSelect }: Props) {
  if (stages.length === 0) {
    return <p className="muted">No stages yet.</p>;
  }

  return (
    <ul className="stage-list">
      {stages.map((stage) => {
        const selected = stage.id === selectedId;
        return (
          <li key={stage.id}>
            <button
              type="button"
              className={selected ? "stage-item selected" : "stage-item"}
              onClick={() => onSelect(stage.id)}
              aria-pressed={selected}
            >
              <span className="stage-id">{stage.id}</span>
              <span className={`status status-${stage.status}`}>
                {stage.status}
              </span>
              {stage.summary ? (
                <span className="stage-summary">{stage.summary}</span>
              ) : null}
            </button>
          </li>
        );
      })}
    </ul>
  );
}
