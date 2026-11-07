# Issue #266: SDK protocol selector validation

Status: PR in review preparation; not merged.

The SDK compatibility checker now refuses fractional, empty, negative and non-finite protocol selectors, rather than trusting JavaScript numeric coercion. The official plugin release and cross-repository SDK tagging requirements remain separate.

Run `node --test scripts/check-sdk-compat.test.mjs`, `node scripts/check-sdk-compat.mjs`, and the policy CI gate.
