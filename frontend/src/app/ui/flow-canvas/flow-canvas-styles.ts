/**
 * Canonical graph canvas styles (Flow Editor look). Every component that draws a
 * graph includes this string in its `styles` so nodes, ports, connectors,
 * selection and state tones are defined once. Colors come from the `--fs-*`
 * design tokens in `styles/_tokens.scss`, so light and dark themes follow.
 */
export const FLOW_CANVAS_STYLES = `
 .flow-edge{fill:none;stroke:var(--fs-edge);stroke-width:2.2}
 .edge-arrow{fill:var(--fs-edge)}
 .wire-ghost{fill:none;stroke:var(--fs-wire-ghost);stroke-width:2.5;stroke-dasharray:6 4}
 .wire-target{fill:none;stroke:var(--fs-wire-ghost);stroke-width:3}
 .flow-node{fill:var(--fs-node-bg);stroke:var(--fs-node-border);stroke-width:1.5;filter:drop-shadow(0 2px 4px rgba(15,23,42,.12))}
 .flow-node.selected{fill:var(--fs-node-selected-bg);stroke:var(--fs-node-selected);stroke-width:2.5}
 [data-canvas-node]:focus-visible .flow-node{stroke:var(--fs-node-selected);stroke-width:3}
 [data-canvas-node]:focus,[data-canvas-node]:focus-visible,.exec-node:focus{outline:none}
 .flow-node-accent{fill:var(--fs-accent)}
 .flow-node-accent[data-type=trigger]{fill:#0ea5e9}.flow-node-accent[data-type=output]{fill:#10b981}.flow-node-accent[data-type=condition],.flow-node-accent[data-type=loop]{fill:#f59e0b}
 .flow-node-accent[data-type=workspace_write],.flow-node-accent[data-type=emit_event]{fill:#ef4444}.flow-node-accent[data-type=http_get],.flow-node-accent[data-type=web_search],.flow-node-accent[data-type=web_crawl]{fill:#14b8a6}
 .flow-node-accent[data-type=watchlist_quotes],.flow-node-accent[data-type=technical_scan],.flow-node-accent[data-type=fundamentals],.flow-node-accent[data-type=sec_filings],.flow-node-accent[data-type=news],.flow-node-accent[data-type=leaderboard],.flow-node-accent[data-type=insider_activity],.flow-node-accent[data-type=wealth_plan],.flow-node-accent[data-type=backtest],.flow-node-accent[data-type=macro_regime],.flow-node-accent[data-type=portfolio_check],.flow-node-accent[data-type=corporate_events]{fill:#f59e0b}.flow-node-accent[data-type=screen],.flow-node-accent[data-type=only_new],.flow-node-accent[data-type=report_table]{fill:#8b5cf6}.flow-node-accent[data-type=notify]{fill:#ef4444}
 .node-type{fill:var(--fs-node-sub);font-weight:600;letter-spacing:.04em}
 .node-label{fill:var(--fs-node-text);font-weight:600}
 .pin-label{fill:var(--fs-node-sub)}
 .canvas-port{touch-action:none;stroke:var(--fs-port-stroke);stroke-width:2;cursor:crosshair}
 .input-port,.pin-in{fill:var(--fs-port-in)}.output-port,.pin-out{fill:var(--fs-port-out)}
 .port-active,.canvas-port:focus-visible{stroke:var(--fs-wire-ghost);stroke-width:4;outline:none}
 .canvas-surface{background:radial-gradient(var(--fs-canvas-dot) 1px,transparent 1px) 0 0/20px 20px,var(--fs-canvas-bg)}
 .flow-edge.kind-exec{stroke-dasharray:7 4}.flow-edge.kind-event{stroke-dasharray:2 5;stroke-linecap:round}
 .flow-node.tone-ok{fill:var(--fs-pos-soft);stroke:var(--fs-pos)}
 .flow-node.tone-fail{fill:var(--fs-neg-soft);stroke:var(--fs-neg)}
 .flow-node.tone-run{fill:var(--fs-warn-soft);stroke:var(--fs-warn)}
 .flow-node.tone-skip{opacity:.6}
 .flow-node.selected.tone-ok,.flow-node.selected.tone-fail,.flow-node.selected.tone-run{stroke:var(--fs-node-selected)}
 .node-sub{fill:var(--fs-node-sub)}
 .canvas-navigation{display:flex;align-items:center;gap:4px;flex-wrap:wrap;padding:8px;background:var(--fs-surface);border-bottom:1px solid var(--fs-border)}
 .canvas-navigation button,.canvas-context-menu button{display:inline-flex;align-items:center;gap:6px;background:var(--fs-surface);color:var(--fs-text-2);border:1px solid var(--fs-border-strong);border-radius:6px;min-height:32px;padding:4px 10px;font:inherit;font-size:.8125rem}
 .canvas-navigation button:hover,.canvas-context-menu button:hover{background:var(--fs-surface-3);color:var(--fs-text)}
 .canvas-navigation button:focus-visible,.canvas-context-menu button:focus-visible{outline:2px solid var(--fs-accent);outline-offset:1px}
 .canvas-navigation span{min-width:48px;text-align:center;font-variant-numeric:tabular-nums;font-size:.8125rem;color:var(--fs-muted)}
 @media(forced-colors:active){.flow-node{stroke:CanvasText}.flow-node.selected{stroke:Highlight}.flow-edge{stroke:CanvasText}}
`;
