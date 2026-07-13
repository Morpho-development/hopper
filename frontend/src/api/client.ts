const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";

export type AuthUser = {
  id: string;
  login: string;
};

export type AuthResponse = {
  accessToken: string;
  expiresIn: number;
  user: AuthUser;
};

export type FileItem = {
  id: string;
  type: "file" | "folder";
  name: string;
  mimeType: string;
  sizeBytes: number;
  createdAt: string;
  updatedAt: string;
};

export type StorageStatus = {
  usedBytes: number;
  quotaBytes: number;
  availableBytes: number;
};

export type FileListResponse = {
  items: FileItem[];
  page: number;
  pageSize: number;
  total: number;
  storage: StorageStatus;
};

export type Message = {
  id: string;
  body: string;
  createdAt: string;
  updatedAt: string;
};

export type CreatedShare = {
  url: string;
  expiresAt: string;
  maxDownloads: number | null;
};

type ApiErrorBody = {
  error?: {
    code?: string;
    message?: string;
  };
};

export async function register(login: string, password: string): Promise<AuthUser> {
  const response = await request<{ user: AuthUser }>("/api/auth/register", {
    method: "POST",
    body: JSON.stringify({ login, password })
  });
  return response.user;
}

export async function login(login: string, password: string): Promise<AuthResponse> {
  return request<AuthResponse>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ login, password })
  });
}

export function refreshSession(): Promise<AuthResponse> {
  return request<AuthResponse>("/api/auth/refresh", { method: "POST" });
}

export async function logout(): Promise<void> {
  await request<void>("/api/auth/logout", { method: "POST" });
}

export async function me(accessToken: string): Promise<AuthUser> {
  const response = await request<{ user: AuthUser }>("/api/auth/me", {
    headers: {
      Authorization: `Bearer ${accessToken}`
    }
  });
  return response.user;
}

export function updateCredentials(accessToken: string, currentPassword: string, newLogin: string, newPassword?: string): Promise<AuthResponse> {
  return request<AuthResponse>("/api/auth/me", {
    method: "PATCH",
    headers: authHeaders(accessToken),
    body: JSON.stringify({
      currentPassword,
      newLogin,
      ...(newPassword ? { newPassword } : {})
    })
  });
}

export function listFiles(accessToken: string, folderID?: string): Promise<FileListResponse> {
  const query = new URLSearchParams({ pageSize: "200" });
  if (folderID) query.set("folderId", folderID);
  return request<FileListResponse>(`/api/files/?${query}`, {
    headers: authHeaders(accessToken)
  });
}

export async function uploadFile(accessToken: string, file: File, folderID?: string): Promise<void> {
  const body = new FormData();
  body.append("file", file);
  if (folderID) body.append("folderId", folderID);
  await request<unknown>("/api/files/upload", {
    method: "POST",
    headers: authHeaders(accessToken),
    body
  });
}

export async function createFolder(accessToken: string, name: string, parentID?: string): Promise<void> {
  await request<unknown>("/api/folders/", {
    method: "POST",
    headers: authHeaders(accessToken),
    body: JSON.stringify({ name, parentId: parentID ?? null })
  });
}

export async function renameFile(accessToken: string, id: string, name: string): Promise<void> {
  await request<unknown>(`/api/files/${id}`, {
    method: "PATCH",
    headers: authHeaders(accessToken),
    body: JSON.stringify({ name })
  });
}

export async function downloadFile(accessToken: string, file: FileItem): Promise<void> {
  const response = await fetch(`${API_BASE}/api/files/${file.id}`, {
    headers: authHeaders(accessToken)
  });
  if (!response.ok) {
    throw new Error(await responseMessage(response));
  }
  const url = URL.createObjectURL(await response.blob());
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = file.name;
  anchor.click();
  URL.revokeObjectURL(url);
}

export async function fetchPreview(accessToken: string, id: string): Promise<{ blob: Blob; contentType: string }> {
  const response = await fetch(`${API_BASE}/api/files/${id}/preview`, {
    headers: authHeaders(accessToken),
    credentials: "same-origin"
  });
  if (!response.ok) {
    throw new Error(await responseMessage(response));
  }
  return {
    blob: await response.blob(),
    contentType: response.headers.get("Content-Type") ?? "application/octet-stream"
  };
}

export async function deleteFile(accessToken: string, id: string): Promise<void> {
  await request<void>(`/api/files/${id}`, {
    method: "DELETE",
    headers: authHeaders(accessToken)
  });
}

export function purgeStorage(accessToken: string, currentPassword: string, confirmation: string): Promise<{ deletedFiles: number; freedBytes: number }> {
  return request<{ deletedFiles: number; freedBytes: number }>("/api/files/", {
    method: "DELETE",
    headers: authHeaders(accessToken),
    body: JSON.stringify({ currentPassword, confirmation })
  });
}

export async function listMessages(accessToken: string): Promise<Message[]> {
  const response = await request<{ messages: Message[] }>("/api/messages/", { headers: authHeaders(accessToken) });
  return response.messages;
}

export function createMessage(accessToken: string, body: string): Promise<Message> {
  return request<Message>("/api/messages/", {
    method: "POST",
    headers: authHeaders(accessToken),
    body: JSON.stringify({ body })
  });
}

export async function deleteMessage(accessToken: string, id: string): Promise<void> {
  await request<void>(`/api/messages/${id}`, { method: "DELETE", headers: authHeaders(accessToken) });
}

export function createShare(accessToken: string, fileID: string, expiresInSeconds: number, maxDownloads?: number): Promise<CreatedShare> {
  return request<CreatedShare>(`/api/files/${fileID}/share/`, {
    method: "POST",
    headers: authHeaders(accessToken),
    body: JSON.stringify({ expiresInSeconds, ...(maxDownloads ? { maxDownloads } : {}) })
  });
}

function authHeaders(accessToken: string): HeadersInit {
  return { Authorization: `Bearer ${accessToken}` };
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
    credentials: "same-origin"
  });

  if (!response.ok) {
    throw new Error(await responseMessage(response));
  }

  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

async function responseMessage(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as ApiErrorBody;
    return body.error?.message ?? response.statusText ?? "Request failed";
  } catch {
    return response.statusText || "Request failed";
  }
}
