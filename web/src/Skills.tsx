import { confirmDialog } from "./confirm";
import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { api } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Markdown from "./Markdown";
import Switch from "./Switch";
import Menu from "./Menu";

export type Skill = {
  id: string;
  name: string;
  description: string;
  body: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

const example = `---
name: release-notes
description: Use when the user asks for release notes or a changelog entry.
---

Write release notes as a short bulleted list grouped by Added, Changed, and Fixed.
Link each ticket you mention.`;

type SkillSort = "recent" | "name";

export default function Skills({ id, refresh }: { id?: string; refresh: number }) {
  const [skills, setSkills] = useState<Skill[]>();
  const [sort, setSort] = useState<SkillSort>(() =>
    localStorage.getItem("skill-sort") === "name" ? "name" : "recent",
  );
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    api
      .get<Skill[]>("/api/skills")
      .then((list) => {
        setSkills(list);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  const changed = () => setReload((value) => value + 1);

  async function importFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    try {
      const skill = await api.post<Skill>("/api/skills/import", { text: await file.text() });
      setError(undefined);
      navigate(`/ai/skills/${skill.id}`);
    } catch (reason) {
      setError(reason);
    }
  }

  async function toggle(skill: Skill) {
    try {
      await api.put(`/api/skills/${skill.id}`, { enabled: !skill.enabled });
      changed();
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove(skill: Skill) {
    if (!(await confirmDialog(`Delete skill "${skill.name}"?`))) return;
    try {
      await api.delete(`/api/skills/${skill.id}`);
      changed();
    } catch (reason) {
      setError(reason);
    }
  }

  if (id) {
    const skill = skills?.find((s) => s.id === id);
    if (id !== "new" && !skill) {
      return skills ? (
        <div className="stack">
          <BackLink />
          <p className="muted">This skill no longer exists.</p>
        </div>
      ) : null;
    }
    return (
      <SkillEditor
        key={id}
        skill={skill}
        onSaved={(saved) => {
          changed();
          if (!skill) navigate("/ai/skills");
        }}
        onDeleted={() => navigate("/ai/skills")}
      />
    );
  }

  return (
    <div className="stack">
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>Skills</h1>
        </div>
        <div className="actions">
          <button onClick={() => fileInput.current?.click()}>
            <Icon name="upload" /> Import SKILL.md
          </button>
          <input ref={fileInput} type="file" accept=".md,text/markdown" hidden onChange={importFile} />
          <button className="primary" onClick={() => navigate("/ai/skills/new")}>
            <Icon name="plus" /> New skill
          </button>
        </div>
      </div>
      <p className="muted">
        Enabled skills are offered to every chat. The model reads each skill's description and loads the
        instructions when a task matches. Changes apply to new chats.
      </p>
      <ErrorText error={error} />

      {skills?.length === 0 && (
        <section className="panel skill-empty">
          <span className="skill-empty-mark" aria-hidden="true">
            <Icon name="sparkles" />
          </span>
          <h2>No skills yet</h2>
          <p className="muted">
            A skill is a set of instructions the model loads when a task matches its description. Write one here
            or import a <code>SKILL.md</code> file.
          </p>
          <pre className="mcp-config">
            <code>{example}</code>
          </pre>
          <div className="actions">
            <button onClick={() => fileInput.current?.click()}>
              <Icon name="upload" /> Import SKILL.md
            </button>
            <button className="primary" onClick={() => navigate("/ai/skills/new")}>
              <Icon name="plus" /> New skill
            </button>
          </div>
        </section>
      )}

      {!!skills?.length && (
        <div className="list-toolbar">
          <span className="muted">
            {skills.length} skill{skills.length === 1 ? "" : "s"}
          </span>
          <SortMenu
            value={sort}
            onChange={(value) => {
              localStorage.setItem("skill-sort", value);
              setSort(value);
            }}
          />
        </div>
      )}

      {!!skills?.length && (
        <ul className="panel skill-list" aria-label="Skills">
          {[...skills]
            .sort((a, b) =>
              sort === "name" ? a.name.localeCompare(b.name) : b.created_at.localeCompare(a.created_at),
            )
            .map((skill) => (
            <li key={skill.id} className={skill.enabled ? "" : "disabled"}>
              <button className="skill-item" onClick={() => navigate(`/ai/skills/${skill.id}`)}>
                <span className="skill-name">{skill.name}</span>
                <span className="skill-description">{skill.description}</span>
              </button>
              <Switch
                checked={skill.enabled}
                onChange={() => toggle(skill)}
                label={`Enable ${skill.name}`}
                showLabel={false}
              />
              <RowMenu
                label={`More actions for ${skill.name}`}
                enabled={skill.enabled}
                onToggle={() => toggle(skill)}
                onEdit={() => navigate(`/ai/skills/${skill.id}`)}
                onRemove={() => remove(skill)}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function RowMenu({
  label,
  enabled,
  onToggle,
  onEdit,
  onRemove,
}: {
  label: string;
  enabled: boolean;
  onToggle: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  return (
    <Menu label={label} buttonClassName="icon ghost" button={<Icon name="more" />}>
      {(close) => (
        <>
          <button role="menuitem" onClick={() => (close(), onToggle())}>
            <Icon name={enabled ? "pause" : "play"} /> {enabled ? "Disable skill" : "Enable skill"}
          </button>
          <button role="menuitem" onClick={() => (close(false), onEdit())}>
            <Icon name="pencil" /> Edit
          </button>
          <hr className="menu-divider" role="separator" />
          <button role="menuitem" className="danger-text" onClick={() => (close(), onRemove())}>
            <Icon name="trash" /> Remove
          </button>
        </>
      )}
    </Menu>
  );
}

const sortLabels: Record<SkillSort, string> = { recent: "Most recent", name: "Alphabetical" };

function SortMenu({ value, onChange }: { value: SkillSort; onChange: (value: SkillSort) => void }) {
  return (
    <Menu
      label="Sort skills"
      button={
        <>
          Sort: {sortLabels[value]} <Icon name="chevronDown" />
        </>
      }
    >
      {(close) =>
        (Object.keys(sortLabels) as SkillSort[]).map((option) => (
          <button
            key={option}
            role="menuitemradio"
            aria-checked={option === value}
            onClick={() => {
              onChange(option);
              close();
            }}
          >
            <span className="menu-check">{option === value && <Icon name="check" />}</span>
            {sortLabels[option]}
          </button>
        ))
      }
    </Menu>
  );
}

function BackLink() {
  return (
    <button type="button" className="back" onClick={() => navigate("/ai/skills")}>
      <Icon name="arrowLeft" /> Skills
    </button>
  );
}

function SkillEditor({
  skill,
  onSaved,
  onDeleted,
}: {
  skill?: Skill;
  onSaved: (skill: Skill) => void;
  onDeleted: () => void;
}) {
  const [name, setName] = useState(skill?.name ?? "");
  const [description, setDescription] = useState(skill?.description ?? "");
  const [body, setBody] = useState(skill?.body ?? "");
  const [enabled, setEnabled] = useState(skill?.enabled ?? true);
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const dirty =
    !skill ||
    name !== skill.name ||
    description !== skill.description ||
    body !== skill.body ||
    enabled !== skill.enabled;

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const fields = { name: name.trim(), description: description.trim(), body, enabled };
      const result = skill
        ? await api.put<Skill>(`/api/skills/${skill.id}`, fields)
        : await api.post<Skill>("/api/skills", fields);
      setName(result.name);
      setDescription(result.description);
      setBody(result.body);
      setEnabled(result.enabled);
      setError(undefined);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved(result);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!skill || !(await confirmDialog(`Delete skill "${skill.name}"?`))) return;
    try {
      await api.delete(`/api/skills/${skill.id}`);
      onDeleted();
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <div className="stack">
      <BackLink />
      <div className="page-heading">
        <h1>{skill ? skill.name : "New skill"}</h1>
      </div>
    <form className="panel skill-editor" onSubmit={submit} aria-label={skill ? `Edit ${skill.name}` : "New skill"}>
      <div className="skill-fields">
        <label>
          Name
          <input
            autoFocus
            required
            maxLength={64}
            pattern="[a-z0-9]+(-[a-z0-9]+)*"
            title="Lowercase letters, digits, and hyphens"
            placeholder="release-notes"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label className="grow">
          Description
          <input
            required
            maxLength={1024}
            placeholder="Use when the user asks for…"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </label>
      </div>
      <div className="skill-body">
        <label>
          Instructions (Markdown)
          <textarea rows={18} spellCheck={false} value={body} onChange={(event) => setBody(event.target.value)} />
        </label>
        <section className="skill-preview" aria-label="Preview">
          <p className="eyebrow">Preview</p>
          {body.trim() ? <Markdown text={body} /> : <p className="muted">Nothing to preview yet.</p>}
        </section>
      </div>
      <ErrorText error={error} />
      <div className="connector-buttons">
        <Switch checked={enabled} onChange={setEnabled} label="Enabled in chats" />
        <button className="primary" disabled={!dirty}>
          {saved ? "Saved" : skill ? "Save" : "Create skill"}
        </button>
        {skill && (
          <a className="button-link" href={`/api/skills/${skill.id}/export`} download="SKILL.md">
            <Icon name="download" /> Export
          </a>
        )}
        {skill && <IconButton type="button" className="danger-text" icon="trash" label={`Delete ${skill.name}`} onClick={remove} />}
      </div>
    </form>
    </div>
  );
}
