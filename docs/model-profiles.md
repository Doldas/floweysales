# Model profiles and saved teams

A **SYS_ADMIN** prepares a few named model choices on the configured endpoint, such as "Fast and cheap" or "Deep research". **Every user** then picks a profile by name for a chat, and for each character of an agent team. Users never see the endpoint or the key and cannot pick a raw model; an administrator who names a model explicitly keeps that ability.

## What a profile holds

| Field | Meaning |
| --- | --- |
| `name`, `description` | What users see. Names are unique. |
| `model` | A model of the default endpoint: the server's default model or one in the model catalog (Settings → API integrations). |
| `fallback_model` | Optional. If the endpoint says the model is not available (404, "model not found"), the run continues with this one, and says so. A switch is not counted as a failed attempt. |
| `supports_tools`, `context_window`, `cost_input_per_mtok`, `cost_output_per_mtok` | What is known. Unknown is `null` and is shown as unknown, never as zero. A model with `supports_tools: false` runs without tools, and its card says so. |
| `max_steps`, `max_tool_calls` | Lower a run's budgets for this profile (never raise them past the server limits). |
| `enabled`, `is_default`, `position` | Disabled profiles are hidden. One profile can be the default; it applies when a user picks none. |

At most 20 profiles. A disabled or deleted profile is never swapped silently: a chat or team that names it is refused with a clear message, and a saved team shows which member lost its profile (it then uses the default).

## API

| Request | Who | Purpose |
| --- | --- | --- |
| `GET /api/ai/profiles` | everyone | Enabled profiles: name, description, model, capabilities, costs, default. No endpoint, key or fallback. |
| `GET/POST /api/admin/ai/profiles`, `PUT/DELETE /api/admin/ai/profiles/{id}` | SYS_ADMIN | Manage profiles (with authentication off, the single local user may). |
| `POST /api/ai/chat` with `profile_id` | everyone | Runs the chat on that profile. |
| `POST /api/ai/team` with `members: [{persona, profile_id?}]`, `profile_id?` or `team_id` | everyone | Each member uses its own profile; `profile_id` is the default for members without one; `team_id` runs a saved team. |
| `GET/POST /api/ai/teams`, `PUT/DELETE /api/ai/teams/{id}` | each user, own teams | Saved teams: name, whether the chair runs, and each character with its profile. At most 20 per user. |

The stream names the model: `run_start` carries `model` at the start of a run and again after a fallback, and `member_start` carries each member's model. A `notice` with code `fallback` announces a switch. The chair of a team uses the server's default model.

## Limits of this first version

- One endpoint per server. A profile chooses among the models that endpoint serves. Several endpoints would add an endpoint reference to a profile and need no change for users.
- Costs are shown, not enforced: the model client does not yet read token usage, so there is no per-team cost ceiling.
- Flow agent nodes still take a model name from a person who can edit flows; they do not use profiles yet.
