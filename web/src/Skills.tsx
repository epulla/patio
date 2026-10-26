import { confirmDialog } from "./confirm";
import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { api } from "./api";
import { ErrorText, navigate } from "./App";
import { Icon, IconButton } from "./icons";
import Markdown from "./Markdown";
import Switch from "./Switch";
import Menu from "./Menu";
import History from "./History";
import { lang, useText } from "./i18n";

const text = { en: { skills: "Skills", memory: "Memory", skill: "skill", new: "New", import: "Import SKILL.md", changes: "Changes apply to new chats.", skillsHelp: "Skills are how-to instructions for a kind of task. Every chat sees each enabled skill's name and description, and the model loads the full instructions only when a task matches.", memoriesHelp: "Memories are your preferences and facts. Every chat sees each enabled memory and follows it. The model saves new ones when you correct it or ask it to remember something.", memoriesEmpty: "No memories yet", memoriesHint: 'Tell the model "remember that…" in any chat, or add one here.', skillsEmpty: "No skills yet", skillHint: "A skill is a set of instructions the model loads when a task matches its description. Write one here or import a", countSkill: (n: number) => `${n} skill${n === 1 ? "" : "s"}`, countMemory: (n: number) => `${n} memor${n === 1 ? "y" : "ies"}`, enable: "Enable", more: "More actions for", disableSkill: "Disable skill", enableSkill: "Enable skill", edit: "Edit", remove: "Remove", recent: "Most recent", alphabetical: "Alphabetical", sort: "Sort skills", sortPrefix: "Sort:", back: "Skills", deleted: "This skill no longer exists.", delete: "Delete skill", editLabel: "Edit", name: "Name", nameTitle: "Lowercase letters, digits, and hyphens", type: "Type", description: "Description", descriptionPlaceholder: "Use when the user asks for…", instructions: "Instructions (Markdown)", preview: "Preview", nothing: "Nothing to preview yet.", enabled: "Enabled in chats", saved: "Saved", save: "Save", createSkill: "Create skill", export: "Export", previewLabel: "Preview" }, es: { skills: "Habilidades", memory: "Memoria", skill: "habilidad", new: "Nueva", import: "Importar SKILL.md", changes: "Los cambios se aplican a los chats nuevos.", skillsHelp: "Las habilidades son instrucciones para realizar tareas. Cada chat ve el nombre y la descripción de cada habilidad activa; el modelo carga las instrucciones completas solo cuando coinciden con una tarea.", memoriesHelp: "Los recuerdos contienen tus preferencias y datos. Cada chat ve y sigue cada recuerdo activo. El modelo guarda nuevos recuerdos cuando lo corriges o le pides que recuerde algo.", memoriesEmpty: "Aún no hay recuerdos", memoriesHint: 'Dile al modelo «recuerda que…» en cualquier chat, o agrega uno aquí.', skillsEmpty: "Aún no hay habilidades", skillHint: "Una habilidad es un conjunto de instrucciones que el modelo carga cuando una tarea coincide con su descripción. Escribe una aquí o importa un", countSkill: (n: number) => `${n} ${n === 1 ? "habilidad" : "habilidades"}`, countMemory: (n: number) => `${n} ${n === 1 ? "recuerdo" : "recuerdos"}`, enable: "Activar", more: "Más acciones para", disableSkill: "Desactivar habilidad", enableSkill: "Activar habilidad", edit: "Editar", remove: "Eliminar", recent: "Más recientes", alphabetical: "Alfabético", sort: "Ordenar habilidades", sortPrefix: "Ordenar:", back: "Habilidades", deleted: "Esta habilidad ya no existe.", delete: "Eliminar habilidad", editLabel: "Editar", name: "Nombre", nameTitle: "Letras minúsculas, números y guiones", type: "Tipo", description: "Descripción", descriptionPlaceholder: "Usar cuando el usuario pida…", instructions: "Instrucciones (Markdown)", preview: "Vista previa", nothing: "Aún no hay nada para previsualizar.", enabled: "Activa en chats", saved: "Guardado", save: "Guardar", createSkill: "Crear habilidad", export: "Exportar", previewLabel: "Vista previa" } };

export type Skill = {
  id: string;
  name: string;
  description: string;
  body: string;
  enabled: boolean;
  kind: "skill" | "memory";
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
  const t = useText(text);
  const [skills, setSkills] = useState<Skill[]>();
  const [sort, setSort] = useState<SkillSort>(() =>
    localStorage.getItem("skill-sort") === "name" ? "name" : "recent",
  );
  const [kind, setKind] = useState<Skill["kind"]>(() =>
    localStorage.getItem("skill-tab") === "memory" ? "memory" : "skill",
  );
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);
  const fileInput = useRef<HTMLInputElement>(null);
  const shown = skills?.filter((skill) => skill.kind === kind);

  function chooseKind(value: Skill["kind"]) {
    localStorage.setItem("skill-tab", value);
    setKind(value);
  }

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
          <p className="muted">{t.deleted}</p>
        </div>
      ) : null;
    }
    return (
      <SkillEditor
        key={id}
        skill={skill}
        defaultKind={kind}
        refresh={refresh + reload}
        onSaved={(saved) => {
          chooseKind(saved.kind);
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
          <h1>{t.skills}</h1>
        </div>
        <div className="actions">
          {kind === "skill" && (
            <button onClick={() => fileInput.current?.click()}>
              <Icon name="upload" /> {t.import}
            </button>
          )}
          <input ref={fileInput} type="file" accept=".md,text/markdown" hidden onChange={importFile} />
          <button className="primary" onClick={() => navigate("/ai/skills/new")}>
            <Icon name="plus" /> {t.new} {kind === "skill" ? t.skill : t.memory}
          </button>
        </div>
      </div>
      <div className="tabs" role="tablist">
        {(["skill", "memory"] as const).map((value) => (
          <button key={value} role="tab" aria-selected={kind === value} onClick={() => chooseKind(value)}>
            {value === "skill" ? t.skills : t.memory}
          </button>
        ))}
      </div>
      <p className="muted">
        {kind === "skill" ? t.skillsHelp : t.memoriesHelp}{" "}
        {t.changes}
      </p>
      <ErrorText error={error} />

      {shown?.length === 0 && kind === "memory" && (
        <section className="panel skill-empty">
          <h2>{t.memoriesEmpty}</h2>
          <p className="muted">{t.memoriesHint}</p>
        </section>
      )}

      {shown?.length === 0 && kind === "skill" && (
        <section className="panel skill-empty">
          <span className="skill-empty-mark" aria-hidden="true">
            <Icon name="sparkles" />
          </span>
          <h2>{t.skillsEmpty}</h2>
          <p className="muted">
            {t.skillHint} <code>SKILL.md</code>.
          </p>
          <pre className="mcp-config">
            <code>{example}</code>
          </pre>
          <div className="actions">
            <button onClick={() => fileInput.current?.click()}>
              <Icon name="upload" /> {t.import}
            </button>
            <button className="primary" onClick={() => navigate("/ai/skills/new")}>
              <Icon name="plus" /> {t.new} {t.skill}
            </button>
          </div>
        </section>
      )}

      {!!shown?.length && (
        <div className="list-toolbar">
          <span className="muted">
            {kind === "skill"
              ? t.countSkill(shown.length)
              : t.countMemory(shown.length)}
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

      {!!shown?.length && (
        <ul className="panel skill-list" aria-label={kind === "skill" ? t.skills : "Recuerdos"}>
          {shown
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
                label={`${t.enable} ${skill.name}`}
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
  const t = useText(text);
  return (
    <Menu label={label} buttonClassName="icon ghost" button={<Icon name="more" />}>
      {(close) => (
        <>
          <button role="menuitem" onClick={() => (close(), onToggle())}>
            <Icon name={enabled ? "pause" : "play"} /> {enabled ? t.disableSkill : t.enableSkill}
          </button>
          <button role="menuitem" onClick={() => (close(false), onEdit())}>
            <Icon name="pencil" /> {t.edit}
          </button>
          <hr className="menu-divider" role="separator" />
          <button role="menuitem" className="danger-text" onClick={() => (close(), onRemove())}>
            <Icon name="trash" /> {t.remove}
          </button>
        </>
      )}
    </Menu>
  );
}

const sortLabels = { en: { recent: "Most recent", name: "Alphabetical" }, es: { recent: "Más recientes", name: "Alfabético" } };

function SortMenu({ value, onChange }: { value: SkillSort; onChange: (value: SkillSort) => void }) {
  const t = useText(text);
  const labels = sortLabels[lang()];
  return (
    <Menu
      label={t.sort}
      button={
        <>
          {t.sortPrefix} {labels[value]} <Icon name="chevronDown" />
        </>
      }
    >
      {(close) =>
        (Object.keys(labels) as SkillSort[]).map((option) => (
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
            {labels[option]}
          </button>
        ))
      }
    </Menu>
  );
}

function BackLink() {
  const t = useText(text);
  return (
    <button type="button" className="back" onClick={() => navigate("/ai/skills")}>
      <Icon name="arrowLeft" /> {t.back}
    </button>
  );
}

function SkillEditor({
  skill,
  defaultKind,
  refresh,
  onSaved,
  onDeleted,
}: {
  skill?: Skill;
  defaultKind: Skill["kind"];
  refresh: number;
  onSaved: (skill: Skill) => void;
  onDeleted: () => void;
}) {
  const t = useText(text);
  const [name, setName] = useState(skill?.name ?? "");
  const [description, setDescription] = useState(skill?.description ?? "");
  const [body, setBody] = useState(skill?.body ?? "");
  const [enabled, setEnabled] = useState(skill?.enabled ?? true);
  const [kind, setKind] = useState<Skill["kind"]>(skill?.kind ?? defaultKind);
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const dirty =
    !skill ||
    name !== skill.name ||
    description !== skill.description ||
    body !== skill.body ||
    enabled !== skill.enabled ||
    kind !== skill.kind;

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const fields = { name: name.trim(), description: description.trim(), body, enabled, kind };
      const result = skill
        ? await api.put<Skill>(`/api/skills/${skill.id}`, fields)
        : await api.post<Skill>("/api/skills", fields);
      setName(result.name);
      setDescription(result.description);
      setBody(result.body);
      setEnabled(result.enabled);
      setKind(result.kind);
      setError(undefined);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved(result);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!skill || !(await confirmDialog(`${t.delete} «${skill.name}»?`))) return;
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
        <h1>{skill ? skill.name : `${t.new} ${kind === "skill" ? t.skill : t.memory}`}</h1>
      </div>
    <form className="panel skill-editor" onSubmit={submit} aria-label={skill ? `${t.edit} ${skill.name}` : `${t.new} ${t.skill}`}>
      <div className="skill-fields">
        <label>
          {t.name}
          <input
            autoFocus
            required
            maxLength={64}
            pattern="[a-z0-9]+(-[a-z0-9]+)*"
            title={t.nameTitle}
            placeholder="release-notes"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          {t.type}
          <select value={kind} onChange={(event) => setKind(event.target.value as Skill["kind"])}>
            <option value="skill">{t.skill}</option>
            <option value="memory">{t.memory}</option>
          </select>
        </label>
        <label className="grow">
          {t.description}
          <input
            required
            maxLength={1024}
            placeholder={t.descriptionPlaceholder}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </label>
      </div>
      <div className="skill-body">
        <label>
          {t.instructions}
          <textarea rows={18} spellCheck={false} value={body} onChange={(event) => setBody(event.target.value)} />
        </label>
        <section className="skill-preview" aria-label={t.previewLabel}>
          <p className="eyebrow">{t.preview}</p>
          {body.trim() ? <Markdown text={body} /> : <p className="muted">{t.nothing}</p>}
        </section>
      </div>
      <ErrorText error={error} />
      <div className="connector-buttons">
        <Switch checked={enabled} onChange={setEnabled} label={t.enabled} />
        <button className="primary" disabled={!dirty}>
          {saved ? t.saved : skill ? t.save : t.createSkill}
        </button>
        {skill && (
          <a className="button-link" href={`/api/skills/${skill.id}/export`} download="SKILL.md">
            <Icon name="download" /> {t.export}
          </a>
        )}
        {skill && <IconButton type="button" className="danger-text" icon="trash" label={`${t.delete} ${skill.name}`} onClick={remove} />}
      </div>
    </form>
      {skill && (
        <History<Skill>
          itemType="skill"
          itemId={skill.id}
          refresh={refresh}
          onRestored={(restored) => {
            setDescription(restored.description);
            setBody(restored.body);
            onSaved(restored);
          }}
        />
      )}
    </div>
  );
}
