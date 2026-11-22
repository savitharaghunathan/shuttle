export type Stage = {
  id: string;
  worker: string;
  status: string;
  attempt: number;
  retries: number;
  summary?: string | null;
  log_path?: string | null;
};

export type Run = {
  id: string;
  template_id: string;
  status: string;
  cwd: string;
  run_dir: string;
  agent?: string;
  model?: string;
  issue_ref?: string;
  parent_run_id?: string;
  remote?: boolean;
  created_at: string;
  updated_at: string;
  current_stage: string;
  outputs_mode?: string;
  warnings?: string[];
  stages: Stage[];
};
