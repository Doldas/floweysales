/**
 * Wording and request building for the vision model settings card. Pure and
 * free of imports so node --test can run it.
 */
export interface VisionLimits {
  max_pages_per_document: number; max_images_per_run: number; max_tokens_per_run: number; timeout_seconds: number; concurrency: number;
}
export interface VisionSettings {
  encryption_ready?: boolean; status: string; enabled: boolean; base_url: string; model: string; fallback_model: string;
  supports_images: boolean; has_key: boolean; verified: boolean; verified_at: string; limits: VisionLimits;
  telemetry: { calls: number; failures: number; cache_hits: number; images: number; tokens: number; last_error_code: string; last_error_at: string };
}

export function statusText(status: string): string {
  switch (status) {
    case 'ready': return 'Ready: scanned pages and picture tables are read with this model';
    case 'needs_test': return 'Saved, not tested: run the image test before it is used';
    case 'disabled': return 'Tested, but switched off';
    case 'no_image_support_declared': return 'Not used: the model is not marked as accepting images';
    default: return 'Not configured: scanned pages are reported as unread';
  }
}

/** What an administrator should do after a failed test; never repeats endpoint detail. */
export function probeAdvice(code: string, message: string): string {
  switch (code) {
    case 'image_rejected': return 'The endpoint refused image content. Choose a vision-capable model or endpoint.';
    case 'wrong_answer': return 'The model accepted the image but did not describe it correctly, so it stays off. It may be a text-only model.';
    case 'auth': return 'The endpoint refused the credentials. Enter the key again.';
    case 'model_unavailable': return 'The endpoint does not serve this model. Check the model name or set a fallback.';
    case 'unreachable': return 'The endpoint could not be reached. Check the address.';
    case 'timeout': return 'The test timed out.';
    default: return message || 'The test failed.';
  }
}

export interface FormState {
  enabled: boolean; baseURL: string; model: string; fallback: string; supportsImages: boolean; apiKey: string; limits: VisionLimits;
}

/** The PUT body. A blank key is left out so the saved key is kept; a key is never read back. */
export function visionRequestBody(f: FormState): Record<string, unknown> {
  const body: Record<string, unknown> = {
    enabled: f.enabled, base_url: f.baseURL.trim(), model: f.model.trim(), fallback_model: f.fallback.trim(),
    supports_images: f.supportsImages, limits: f.limits,
  };
  if (f.apiKey.trim()) body['api_key'] = f.apiKey.trim();
  return body;
}

/** An RFC 3339 time as UTC text: 2026-11-06 14:05 UTC. */
export function utcTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString().slice(0, 16).replace('T', ' ') + ' UTC';
}
