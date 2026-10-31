# Security policy

Please report vulnerabilities privately to the maintainer through GitHub's private vulnerability reporting if enabled, or open a minimal issue without secrets/exploit details to request a private channel.

## Scope

Financial tools can influence significant decisions. Data poisoning, stale prices, incorrect currency/period aggregation, leaked credentials and autonomous execution are considered security and safety risks.

- Keep all account/provider tokens on the server, never in committed config or Pages assets.
- Do not add live trading credentials to sample configs or GitHub Actions.
- Validate untrusted tool arguments and external-provider responses before use.
- Use data-source error states and `null`, never fake zero values for unavailable quotes.
- Experimental trade signals are research only; no automatic real-money execution is authorized by the current product.
- Unofficial Avanza endpoints must be handled respectfully: observe published usage constraints, do not evade logins or rate limiting.
- Provider plugins are native code running as the backend's OS user. Install them only from a store you trust; the backend verifies each binary's SHA-256 against the store index at install and on every start, passes it only the settings it declares and never backend secrets, and restricts plugin management to authenticated SYS_ADMIN sessions (or the server-side CLI). Report a malicious or vulnerable plugin like any other vulnerability.

See `docs/roadmap.md` for security gates on later execution features.
