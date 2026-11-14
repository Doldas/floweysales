import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, map } from 'rxjs';

import {
  filterParams, type Collection, type CollectionDetail, type CollectionFilter, type CollectionInstrument, type CollectionKind,
  type CollectionMember, type ImportResult, type MemberPage,
} from './collections';

export interface CollectionInput {
  name: string;
  kind?: CollectionKind;
  description?: string;
  category?: string;
  tags?: string[];
}

export interface CollectionPatch {
  name?: string;
  description?: string;
  category?: string;
  tags?: string[];
  archived?: boolean;
}

export interface MemberInput {
  instrument: CollectionInstrument;
  note?: string;
  tags?: string[];
  weight?: number;
}

export interface MemberPatch {
  note?: string;
  tags?: string[];
  weight?: number;
  clear_weight?: boolean;
}

/** The caller's own Market Collections (/api/collections). Ownership is checked by the server. */
@Injectable({ providedIn: 'root' })
export class CollectionsService {
  private readonly http = inject(HttpClient);
  private readonly base = '/api/collections';

  list(filter: CollectionFilter = {}): Observable<Collection[]> {
    return this.http.get<{ collections: Collection[] | null }>(this.base, { params: filterParams(filter) }).pipe(map(r => r.collections ?? []));
  }

  get(id: number, limit = 100, offset = 0): Observable<CollectionDetail> {
    return this.http.get<CollectionDetail>(`${this.base}/${id}`, { params: { limit, offset } })
      .pipe(map(d => ({ ...d, members: { ...d.members, members: d.members.members ?? [] } })));
  }

  /** Every member, page by page, for the composition view (at most 1,000 per collection). */
  members(id: number, limit = 500, offset = 0): Observable<MemberPage> {
    return this.http.get<MemberPage>(`${this.base}/${id}/members`, { params: { limit, offset } })
      .pipe(map(p => ({ ...p, members: p.members ?? [] })));
  }

  create(input: CollectionInput): Observable<Collection> {
    return this.http.post<Collection>(this.base, input);
  }

  update(id: number, patch: CollectionPatch): Observable<Collection> {
    return this.http.patch<Collection>(`${this.base}/${id}`, patch);
  }

  delete(id: number): Observable<void> {
    return this.http.delete<void>(`${this.base}/${id}`);
  }

  addMember(id: number, input: MemberInput): Observable<CollectionMember> {
    return this.http.post<CollectionMember>(`${this.base}/${id}/members`, input);
  }

  updateMember(id: number, memberId: number, patch: MemberPatch): Observable<CollectionMember> {
    return this.http.patch<CollectionMember>(`${this.base}/${id}/members/${memberId}`, patch);
  }

  removeMember(id: number, memberId: number): Observable<void> {
    return this.http.delete<void>(`${this.base}/${id}/members/${memberId}`);
  }

  /** Copies (or moves) a member into another of the caller's collections. Weights never travel. */
  transfer(id: number, memberId: number, to: number, move: boolean): Observable<CollectionMember> {
    return this.http.post<CollectionMember>(`${this.base}/${id}/members/${memberId}/transfer`, { to, move });
  }

  containing(provider: string, symbol: string): Observable<Collection[]> {
    return this.http.get<{ collections: Collection[] | null }>(`${this.base}/containing`, { params: { provider, symbol } })
      .pipe(map(r => r.collections ?? []));
  }

  /** The export document as text, ready to save as a file. No ids: all of the caller's collections. */
  exportAll(ids: number[] = []): Observable<string> {
    const params: Record<string, string> = ids.length ? { ids: ids.join(',') } : {};
    return this.http.get(`${this.base}/export`, { params, responseType: 'text' });
  }

  import(document: unknown, onConflict: 'rename' | 'skip' = 'rename'): Observable<ImportResult[]> {
    return this.http.post<{ results: ImportResult[] | null }>(`${this.base}/import`, document, { params: { on_conflict: onConflict } })
      .pipe(map(r => r.results ?? []));
  }
}
