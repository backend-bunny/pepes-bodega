// Typed client for the Go API (internal/api). Keep types in sync with internal/db/queries.go.

export type Kind = 'image' | 'animation' | 'video';
export type Status = 'pending' | 'processing' | 'ready' | 'failed';

export interface User {
	id: number;
	email: string;
	name: string;
}

export interface MediaItem {
	id: number;
	sha256: string;
	original_filename: string;
	source_device: string;
	uploaded_at: string;
	mime_type: string;
	kind: Kind;
	size_bytes: number;
	width: number | null;
	height: number | null;
	duration_s: number | null;
	status: Status;
	duplicate_of: string | null;
	score?: number;
	match?: 'direct' | 'related';
	/** Direct hits: where the query matched. */
	match_source?: 'text' | 'speech' | 'tag' | 'filename';
	/** Direct text/speech hits: excerpt with ⟦…⟧ around matched words. */
	match_snippet?: string;
	match_ts?: number | null;
}

export interface MediaDetail extends MediaItem {
	tags: { tag: string; score: number; source: string }[];
	frames: { idx: number; ts_s: number; ocr_text: string }[];
	transcript: string | null;
	error: string | null;
	duplicates: MediaItem[] | null;
}

export interface Stats {
	media: number;
	by_status: Partial<Record<Status, number>>;
	near_duplicates: number;
}

export interface ApiToken {
	id: number;
	name: string;
	created_at: string;
	last_used_at: string | null;
}

export class Unauthorized extends Error {}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(path, { credentials: 'same-origin', ...init });
	if (res.status === 401) throw new Unauthorized();
	if (!res.ok) {
		let msg = `${res.status} ${res.statusText}`;
		try {
			msg = (await res.json()).error ?? msg;
		} catch {
			/* not json */
		}
		throw new Error(msg);
	}
	return res.status === 204 ? (undefined as T) : res.json();
}

const json = (body: unknown): RequestInit => ({
	method: 'POST',
	headers: { 'Content-Type': 'application/json' },
	body: JSON.stringify(body)
});

export const api = {
	me: () => req<User>('/api/v1/me'),
	stats: () => req<Stats>('/api/v1/stats'),
	list: (before = 0, limit = 60) =>
		req<{ items: MediaItem[] }>(`/api/v1/media?before=${before}&limit=${limit}`),
	search: (q: string, limit = 120) =>
		req<{ items: MediaItem[]; semantic: boolean }>(
			`/api/v1/search?q=${encodeURIComponent(q)}&limit=${limit}`
		),
	get: (id: number) => req<MediaDetail>(`/api/v1/media/${id}`),
	similar: (id: number, limit = 12) =>
		req<{ items: MediaItem[] }>(`/api/v1/media/${id}/similar?limit=${limit}`),
	remove: (id: number) => req<void>(`/api/v1/media/${id}`, { method: 'DELETE' }),
	tokens: () => req<{ items: ApiToken[] }>('/api/v1/tokens'),
	createToken: (name: string) =>
		req<{ token: ApiToken; secret: string }>('/api/v1/tokens', json({ name })),
	deleteToken: (id: number) => req<void>(`/api/v1/tokens/${id}`, { method: 'DELETE' }),
	logout: () => req<void>('/auth/logout', { method: 'POST' })
};

export const rawUrl = (sha: string) => `/api/v1/blobs/${sha}/raw`;
export const thumbUrl = (sha: string) => `/api/v1/blobs/${sha}/thumb`;

export interface UploadResult {
	media_id: number;
	sha256: string;
	created: boolean;
	new_blob: boolean;
	duplicate: boolean;
}

/** Upload one file with progress reporting (fetch has no upload progress). */
export function upload(file: File, onProgress: (frac: number) => void): Promise<UploadResult> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		const form = new FormData();
		form.append('device', 'web');
		form.append('file', file);
		xhr.open('POST', '/api/v1/media');
		xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
		xhr.onload = () => {
			try {
				const body = JSON.parse(xhr.responseText);
				if (xhr.status >= 300) reject(new Error(body.error ?? xhr.statusText));
				else resolve(body.results[0]);
			} catch (e) {
				reject(e);
			}
		};
		xhr.onerror = () => reject(new Error('network error'));
		xhr.send(form);
	});
}

export interface ProgressEvent {
	stats: Stats;
	/** sha256 -> new status, for the user's own media that changed since the last event. */
	blobs: Record<string, Status>;
}

/** Live processing progress (SSE). Reconnects automatically; returns a closer. */
export function subscribeProgress(onEvent: (ev: ProgressEvent) => void): () => void {
	const es = new EventSource('/api/v1/events');
	es.addEventListener('progress', (e) => onEvent(JSON.parse((e as MessageEvent).data)));
	return () => es.close();
}
