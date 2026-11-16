# ADR 0005: Trading host and broker extensions, paper only

- **Status:** Accepted
- **Date:** 2026-11-16
- **Issue:** #444 (companion: Doldas/floweysales-plugins#14)

## Context

Paper trading lives in the core (`internal/trading`, `/api/orders`): one simulated account with its own
order history, approvals and audit trail. Users want to connect one or several brokers, such as a
broker's paper environment, and to have a separate piece propose orders from a portfolio. No
broker-specific logic may enter the core, and issue #8 states that live-money execution is not approved.

## Decision

1. **Brokers and trading hosts are plugins.** Two new provider types, `broker` and `trading_host`,
   ride on the domain-provider mechanism (ADR 0002): one `domain` wire method, typed operations with
   fixed inputs. The wire protocol is unchanged (still version 1). A separate **contract version**
   (`BrokerContractVersion`, now 1) is declared in the manifest and negotiated at install and at every
   call, so the order vocabulary can change without touching the protocol.
2. **Fixed operation inputs.** Unlike tax, a broker or host may not declare its own inputs: the manifest
   must carry exactly the inputs of `pluginsdk.StandardOperation`. Every broker is therefore called the
   same way and the core stays free of broker names.
3. **Paper only, as a constant.** `brokers.LiveExecutionApproved` is `false` and nothing sets it. The
   gate is applied four times to an order: to the broker's declaration (a broker without a `paper`
   environment is never called), to the account the broker reports, to the order sent (`environment` is
   an input the plugin must reject if it differs) and to the order that comes back. A live account is
   listed so the user sees it, never traded. Turning live execution on needs the approvals tracked in #8
   and a reviewed code change, not a setting.
4. **Explicit routing, no fallback.** An order names its broker, connection and account. There is no
   default and no second broker. A USER must name one of their own connections; the system's own
   settings are for SYS_ADMIN only. Read-only views (accounts, positions) may combine several
   connections, but each row carries its source and its currency, and nothing is added up or converted.
5. **A connection is a user's own instance.** The values of a plugin's secret variables are sealed per
   user and per connection (kind `broker_conn:<user>:<name>`), never returned, and handed only to a
   process of that connection (`plugins.WithUserInstanceNamed`). Two connections of one user, and the
   connections of two users, never share a process.
6. **Idempotent by ledger.** The `broker_orders` table reserves a row per `(user, client_order_id)`
   before the plugin is called. The same id with the same order is answered from the ledger; with a
   different order (route included) it is a conflict. A call with no clear answer leaves the row
   `UNKNOWN`, and sending the same id again is safe because the contract makes the broker idempotent on
   it. Refresh asks the broker for the order by client order id.
7. **Same risk controls as the core.** The kill switch and the order-value limit of the trading settings
   apply to broker orders. A buy larger than the account's buying power is refused. An order whose
   value cannot be judged against a set limit is refused, not waved through.
8. **Strict decoding.** What a plugin answers is decoded with unknown fields disallowed and every value
   checked (finite numbers, known vocabulary, bounded sizes). An answer that is not the order that was
   sent, or is for another account, is a contract violation.
9. **Hosts only propose.** A trading host returns order intents that must name the broker and account it
   was asked about. Nothing is sent; each intent is shown with the reason it would be refused, if any.
10. **No agent access, no generic route.** Brokers are not reachable through `/api/domain-providers` or
    the agent's `call_domain_provider` (`CallDomain` refuses them), and the Research Agent has no broker
    tool. A USER needs the new `trading.paper` permission, which no role has by default.
11. **The in-core paper account stays.** It is listed as the built-in broker `PAPER`. Its orders still go
    through `/api/orders`, which keeps approvals, idempotency and the audit trail unchanged. Moving it
    into a Paper Broker extension is a later step that must preserve those semantics; this ADR only
    adds the contract and the registry.

## Reconciliation (phase 2)

12. **The broker is the source of truth for an order's status.** A background reconciler (every 20 seconds,
    25 orders per round, 20 seconds per call) asks the broker about every order that may still change:
    outcomes that are unknown, rows whose call never finished, and confirmed orders that are still working.
    A row is checked at most every 30 seconds. The same code runs when a user presses Refresh.
13. **It only reads.** Reconciliation calls `broker_orders` with the client order id and never sends, retries
    or cancels an order. It uses the route the order was sent on (the user's connection, or the system's
    settings for an administrator), so each check runs with the right key. An uninstalled broker or a
    removed connection is recorded on the row, never guessed around.
14. **A report that cannot be true changes nothing.** The report must be the order that was sent (account,
    client order id, symbol, side, quantity), fills never go backwards, an order that ended stays as it ended,
    a filled order is filled in full and a partial one is partial. Otherwise the stored order stays as it was and
    the problem is kept on the row (`reconcile_error`) with the time of the check.
15. **Unknown outcomes are settled by the broker's answer.** If the broker has the order the row becomes
    submitted. If it has none, the row stays unknown for 10 minutes and then fails with that reason; a confirmed
    order that disappears is never failed silently, it is reported. A pending row whose call never finished
    becomes unknown first.
16. **Checks of one order never interleave.** A lock per row makes a check re-read the row before it decides,
    so an older report cannot be written over a newer one.

## Consequences

- A broker plugin is a small adapter; a new broker needs no backend release.
- Live trading cannot be enabled by configuration, a plugin or a request, which is the point.
- The SDK gains an additive version (pluginsdk v0.6.0): older backends reject plugins that declare these
  types, so such plugins set `min_app_version`.
- Order status comes from the broker by polling (every 20 seconds at most); there is no streaming of fills
  and no automatic cancellation, for example when the kill switch is set.
- A `broker_orders` implementation must honor the client order id filter and report truthfully; a plugin that
  lets fills go back is shown as a contract problem on the order.
