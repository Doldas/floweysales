import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, map, shareReplay } from 'rxjs';

import type { CollectionNews } from './collection-news';
import type { InstrumentKey, Intelligence, Override, ProfileView, RefreshResult, Taxonomy } from './instruments';

/**
 * Instrument profiles, the platform taxonomy and the caller's own
 * classifications (/api/instruments). Profiles are shared provider facts;
 * classifications are private to the caller.
 */
@Injectable({ providedIn: 'root' })
export class InstrumentsService {
  private readonly http = inject(HttpClient);
  private taxonomy$?: Observable<Taxonomy>;

  /** The fixed taxonomy, asked for once. */
  taxonomy(): Observable<Taxonomy> {
    this.taxonomy$ ??= this.http.get<Taxonomy>('/api/instruments/taxonomy').pipe(shareReplay({ bufferSize: 1, refCount: false }));
    return this.taxonomy$;
  }

  /** The instrument's profile from its own provider; refresh asks the provider again (at most once a minute). */
  profile(key: InstrumentKey, refresh = false): Observable<ProfileView> {
    const params: Record<string, string> = { provider: key.provider, symbol: key.symbol };
    if (refresh) params['refresh'] = '1';
    return this.http.get<ProfileView>('/api/instruments/profile', { params })
      .pipe(map(v => ({ ...v, override: v.override ?? null, classification: { ...v.classification, themes: v.classification.themes ?? [] } })));
  }

  saveClassification(o: Override): Observable<Override | null> {
    return this.http.put<{ override: Override | null }>('/api/instruments/classification', o).pipe(map(r => r.override ?? null));
  }

  deleteClassification(key: InstrumentKey): Observable<void> {
    return this.http.delete<void>('/api/instruments/classification', { params: { provider: key.provider, symbol: key.symbol } });
  }

  /** Cached profiles and classifications of a collection's members. Never asks a provider. */
  intelligence(collectionId: number): Observable<Intelligence> {
    return this.http.get<Intelligence>(`/api/collections/${collectionId}/intelligence`).pipe(map(i => ({
      ...i,
      members: (i.members ?? []).map(m => ({ ...m, classification: { ...m.classification, themes: m.classification.themes ?? [] } })),
      links: i.links ?? [],
      profile_providers: i.profile_providers ?? [],
    })));
  }

  /** Headlines from each member's own provider, related to the collection. Asks providers (cached for a while). */
  collectionNews(collectionId: number): Observable<CollectionNews> {
    return this.http.get<CollectionNews>(`/api/collections/${collectionId}/news`).pipe(map(n => ({
      ...n,
      items: (n.items ?? []).map(i => ({ ...i, matches: (i.matches ?? []).map(m => ({ ...m, members: m.members ?? [] })) })),
      news_providers: n.news_providers ?? [],
    })));
  }

  /** Fetches missing or expired profiles, a bounded batch per call. */
  refreshProfiles(collectionId: number): Observable<RefreshResult> {
    return this.http.post<RefreshResult>(`/api/collections/${collectionId}/profiles/refresh`, {});
  }
}
