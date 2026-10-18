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

See `docs/roadmap.md` for security gates on later execution features.
