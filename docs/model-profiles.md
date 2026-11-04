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
- A flow agent node that still names a raw model keeps working (an ordinary user needs it to be in the model catalog); new choices should use a profile.
- Budget ceilings stop a run only where the endpoint reports token usage; see below.

## Budget ceilings

A team run can carry ceilings, chosen by the user: **tokens** (prompt plus completion, 1,000 to 10,000,000), **cost** in US dollars, and **tool calls** across all members (1 to 500). They are set on the Team page, sent as `max_tokens`, `max_cost` and `max_tool_calls` with `POST /api/ai/team`, and kept with a saved team (the same three fields in `/api/ai/teams`; a request's values replace the saved ones). A delegated run (`delegate_to_team`) uses the saved team's ceilings.

- All members and the chair share one budget. It is checked before each model round and each tool call, so a run can end one round past a token or cost ceiling, never further. Work already done is kept; the member shows an error that names the ceiling, and the final `budget` event says what was used and which ceiling stopped the run.
- The model client asks the endpoint for token usage (`stream_options.include_usage`) and reads `usage` from the reply. An endpoint that rejects the option is asked again without it.
- **Cost** is computed from the prices an administrator entered in the model profiles, per model. A model without both prices, or an endpoint that reports no usage, makes the cost **unknown**: it is shown as unknown, never as zero.
- **Without usage figures the token and cost ceilings cannot be enforced.** The run says so in a notice and carries on; only the tool-call ceiling and the time limits hold. Local servers differ in whether they report usage. This has been tested against recorded replies, not against a live provider.
- Tokens of a failed attempt that the endpoint never reported are not counted.
