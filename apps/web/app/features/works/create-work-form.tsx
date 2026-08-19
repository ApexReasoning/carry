import { type SubmitEvent, useState } from "react";

import { goalInputError } from "./work-input";

type CreateWorkFormProps = {
  busy: boolean;
  onCreate: (goal: string) => Promise<boolean>;
};

export function CreateWorkForm({ busy, onCreate }: CreateWorkFormProps) {
  const [goal, setGoal] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function submit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    const submittedGoal = goal.trim();
    const validationError = goalInputError(goal);
    setError(validationError);
    if (validationError) return;
    if (await onCreate(submittedGoal)) {
      setGoal("");
      setError(null);
    }
  }

  return (
    <form className="create-work" onSubmit={submit}>
      <label htmlFor="work-goal">
        What should Carry take responsibility for?
      </label>
      <p className="field-hint" id="work-goal-hint">
        One clear sentence is enough. You can add facts and corrections once the
        Work exists.
      </p>
      <div className="create-work-row">
        <textarea
          id="work-goal"
          name="work-goal"
          rows={2}
          placeholder="Prepare a concise renewal recommendation before 30 September"
          aria-describedby="work-goal-hint"
          value={goal}
          onChange={(event) => {
            setGoal(event.target.value);
            setError(null);
          }}
          disabled={busy}
          required
        />
        {error ? (
          <p className="alert" role="alert">
            {error}
          </p>
        ) : null}
        <button
          className="primary-button"
          type="submit"
          disabled={busy || !goal.trim()}
        >
          {busy ? "Creating…" : "Create Work"}
        </button>
      </div>
    </form>
  );
}
