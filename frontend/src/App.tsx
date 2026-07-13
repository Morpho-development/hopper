import { ChangeEvent, DragEvent, FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowRight, ChevronLeft, Cloud, Copy, Download, Eye, File, Folder, FolderOpen, FolderPlus, HardDrive, Loader2, Lock, LogOut, MessageSquare, Pencil, RefreshCw, Send, Settings, Share2, Trash2, Upload, User, UserPlus, X } from "lucide-react";
import { AuthResponse, AuthUser, createFolder, createMessage, createShare, CreatedShare, deleteFile, deleteMessage, downloadFile, fetchPreview, FileItem, FileListResponse, listFiles, listMessages, login, logout, Message, purgeStorage, refreshSession, register, renameFile, updateCredentials, uploadFile } from "./api/client";

type Mode = "login" | "register";

type StoredSession = {
  accessToken: string;
  user: AuthUser;
};

export function App() {
  const [mode, setMode] = useState<Mode>("login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [session, setSession] = useState<StoredSession | null>(null);
  const [checkingSession, setCheckingSession] = useState(true);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const sessionCheckStarted = useRef(false);

  const title = useMemo(() => (mode === "login" ? "Sign in" : "Create account"), [mode]);

  useEffect(() => {
    if (sessionCheckStarted.current) return;
    sessionCheckStarted.current = true;
    refreshSession()
      .then((auth) => persistAuth(auth))
      .catch(() => setSession(null))
      .finally(() => setCheckingSession(false));
  }, []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setStatus(null);

    try {
      if (mode === "register") {
        await register(username, password);
        setStatus("Account created. Signing you in...");
      }

      const auth = await login(username, password);
      persistAuth(auth);
      setUsername("");
      setPassword("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  }

  function persistAuth(auth: AuthResponse) {
    const nextSession = {
      accessToken: auth.accessToken,
      user: auth.user
    };
    setSession(nextSession);
  }

  async function signOut() {
    await logout().catch(() => undefined);
    setSession(null);
    setStatus(null);
    setError(null);
  }

  if (checkingSession) {
    return <main className="shell"><Loader2 className="spin" size={28} aria-label="Loading session" /></main>;
  }

  if (session) {
    return <FileManager session={session} onSessionExpired={signOut} onSessionUpdated={persistAuth} />;
  }

  return (
    <main className="shell">
      <section className="auth-panel" aria-label="Authentication">
        <div className="brand-row">
          <span className="brand-mark" aria-hidden="true">
            <Cloud size={24} />
          </span>
          <div>
            <h1>Hopper</h1>
            <p>Private file cloud</p>
          </div>
        </div>

        <>
            <div className="mode-tabs" role="tablist" aria-label="Authentication mode">
              <button
                className={mode === "login" ? "active" : ""}
                type="button"
                role="tab"
                aria-selected={mode === "login"}
                onClick={() => setMode("login")}
              >
                Sign in
              </button>
              <button
                className={mode === "register" ? "active" : ""}
                type="button"
                role="tab"
                aria-selected={mode === "register"}
                onClick={() => setMode("register")}
              >
                Register
              </button>
            </div>

            <form className="auth-form" onSubmit={handleSubmit}>
              <div className="form-heading">
                <h2>{title}</h2>
              </div>

              <label>
                <span>Login</span>
                <div className="input-shell">
                  <User size={18} aria-hidden="true" />
                  <input
                    autoComplete="username"
                    maxLength={32}
                    minLength={3}
                    name="login"
                    onChange={(event) => setUsername(event.target.value)}
                    placeholder="admin"
                    required
                    type="text"
                    value={username}
                  />
                </div>
              </label>

              <label>
                <span>Password</span>
                <div className="input-shell">
                  <Lock size={18} aria-hidden="true" />
                  <input
                    autoComplete={mode === "login" ? "current-password" : "new-password"}
                    minLength={12}
                    name="password"
                    onChange={(event) => setPassword(event.target.value)}
                    placeholder="At least 12 characters"
                    required
                    type="password"
                    value={password}
                  />
                </div>
              </label>

              {error ? <p className="message error">{error}</p> : null}
              {status ? <p className="message status">{status}</p> : null}

              <button className="primary-action" disabled={busy} type="submit">
                {busy ? <Loader2 className="spin" size={18} aria-hidden="true" /> : mode === "login" ? <ArrowRight size={18} aria-hidden="true" /> : <UserPlus size={18} aria-hidden="true" />}
                <span>{busy ? "Working..." : mode === "login" ? "Sign in" : "Create account"}</span>
              </button>
            </form>
          </>
      </section>
    </main>
  );
}

type FileManagerProps = {
  session: StoredSession;
  onSessionExpired: () => void | Promise<void>;
  onSessionUpdated: (auth: AuthResponse) => void;
};

function FileManager({ session, onSessionExpired, onSessionUpdated }: FileManagerProps) {
  const [data, setData] = useState<FileListResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [previewItem, setPreviewItem] = useState<FileItem | null>(null);
  const [messagesOpen, setMessagesOpen] = useState(false);
  const [shareItem, setShareItem] = useState<FileItem | null>(null);
  const [folderStack, setFolderStack] = useState<FileItem[]>([]);
  const [nameDialog, setNameDialog] = useState<{ mode: "folder" | "rename"; item?: FileItem } | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const refresh = useCallback(async () => {
    try {
      setError(null);
      setData(await listFiles(session.accessToken, folderStack.at(-1)?.id));
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to load files";
      if (message.toLowerCase().includes("unauthorized")) {
        onSessionExpired();
        return;
      }
      setError(message);
    }
  }, [folderStack, onSessionExpired, session.accessToken]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function upload(files: FileList | File[]) {
    if (files.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      for (const file of Array.from(files)) {
        await uploadFile(session.accessToken, file, folderStack.at(-1)?.id);
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upload failed");
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  }

  function onFileInput(event: ChangeEvent<HTMLInputElement>) {
    if (event.target.files) void upload(event.target.files);
  }

  function onDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragging(false);
    void upload(event.dataTransfer.files);
  }

  async function remove(item: FileItem) {
    if (!window.confirm(`Delete ${item.name}?`)) return;
    try {
      await deleteFile(session.accessToken, item.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Delete failed");
    }
  }

  async function download(item: FileItem) {
    try {
      await downloadFile(session.accessToken, item);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Download failed");
    }
  }

  const storage = data?.storage;
  const usedPercent = storage?.quotaBytes ? Math.min(100, (storage.usedBytes / storage.quotaBytes) * 100) : 0;

  return (
    <main className="drive-shell">
      <header className="drive-header">
        <div className="brand-row">
          <span className="brand-mark" aria-hidden="true"><Cloud size={24} /></span>
          <div><h1>Hopper</h1><p>Private file cloud</p></div>
        </div>
        <div className="account-actions">
          <span className="account-name"><User size={16} />{session.user.login}</span>
          <button className="icon-button" type="button" title="Messages" aria-label="Messages" onClick={() => setMessagesOpen(true)}><MessageSquare size={19} /></button>
          <button className="icon-button" type="button" title="Account settings" aria-label="Account settings" onClick={() => setSettingsOpen(true)}><Settings size={19} /></button>
          <button className="icon-button" type="button" title="Sign out" aria-label="Sign out" onClick={onSessionExpired}><LogOut size={19} /></button>
        </div>
      </header>

      <section className="drive-content">
        <div className="drive-toolbar">
          <div><div className="folder-heading">{folderStack.length ? <button className="icon-button" type="button" title="Back" aria-label="Back to parent folder" onClick={() => setFolderStack((current) => current.slice(0, -1))}><ChevronLeft size={19} /></button> : null}<div><h2>{folderStack.at(-1)?.name ?? "My files"}</h2><p>{data ? `${data.total} items` : "Loading files..."}</p></div></div></div>
          <div className="toolbar-actions">
            <button className="icon-button" type="button" title="Refresh" aria-label="Refresh files" onClick={() => void refresh()}><RefreshCw size={19} /></button>
            <button className="icon-button" type="button" title="New folder" aria-label="Create folder" onClick={() => setNameDialog({ mode: "folder" })}><FolderPlus size={19} /></button>
            <button className="upload-button" type="button" disabled={busy} onClick={() => inputRef.current?.click()}>
              {busy ? <Loader2 className="spin" size={18} /> : <Upload size={18} />}<span>{busy ? "Uploading" : "Upload"}</span>
            </button>
            <input ref={inputRef} className="visually-hidden" type="file" multiple onChange={onFileInput} />
          </div>
        </div>

        <section className="storage-panel" aria-label="Storage usage">
          <div className="storage-icon"><HardDrive size={22} /></div>
          <div className="storage-details">
            <div className="storage-copy"><strong>Storage</strong><span>{storage ? `${formatBytes(storage.usedBytes)} used · ${formatBytes(storage.availableBytes)} free` : "Calculating..."}</span></div>
            <div className="storage-track"><span style={{ width: `${usedPercent}%` }} /></div>
          </div>
          <strong className="storage-total">{storage ? formatBytes(storage.quotaBytes) : "—"}</strong>
        </section>

        {error ? <p className="message error" role="alert">{error}</p> : null}

        <div className={`drop-zone ${dragging ? "dragging" : ""}`} onDragEnter={(event) => { event.preventDefault(); setDragging(true); }} onDragOver={(event) => event.preventDefault()} onDragLeave={(event) => { if (event.currentTarget === event.target) setDragging(false); }} onDrop={onDrop}>
          {dragging ? <div className="drop-prompt"><Upload size={28} /><strong>Drop files to upload</strong></div> : null}
          {!data ? <div className="empty-state"><Loader2 className="spin" size={24} /><p>Loading your files</p></div> : data.items.length === 0 ? (
            <div className="empty-state"><FolderOpen size={34} /><h3>No files yet</h3><p>Drop files here or use the upload button.</p></div>
          ) : (
            <div className="file-list">
              <div className="file-row file-head"><span>Name</span><span>Size</span><span>Updated</span><span>Actions</span></div>
              {data.items.map((item) => (
                <div className="file-row" key={item.id}>
                  <button className="file-name" type="button" onClick={() => { if (item.type === "folder") setFolderStack((current) => [...current, item]); }}><span className="file-icon">{item.type === "folder" ? <Folder size={19} /> : <File size={19} />}</span><div><strong>{item.name}</strong><small>{item.type === "folder" ? "Folder" : item.mimeType || "File"}</small></div></button>
                  <span data-label="Size">{item.type === "folder" ? "—" : formatBytes(item.sizeBytes)}</span>
                  <span data-label="Updated">{formatDate(item.updatedAt)}</span>
                  <div className="file-actions">
                    {item.type === "folder" ? <button className="icon-button" type="button" title="Open" aria-label={`Open ${item.name}`} onClick={() => setFolderStack((current) => [...current, item])}><FolderOpen size={18} /></button> : <><button className="icon-button" type="button" title="Preview" aria-label={`Preview ${item.name}`} onClick={() => setPreviewItem(item)}><Eye size={18} /></button><button className="icon-button" type="button" title="Rename" aria-label={`Rename ${item.name}`} onClick={() => setNameDialog({ mode: "rename", item })}><Pencil size={18} /></button><button className="icon-button" type="button" title="Share" aria-label={`Share ${item.name}`} onClick={() => setShareItem(item)}><Share2 size={18} /></button><button className="icon-button" type="button" title="Download" aria-label={`Download ${item.name}`} onClick={() => void download(item)}><Download size={18} /></button><button className="icon-button danger" type="button" title="Delete" aria-label={`Delete ${item.name}`} onClick={() => void remove(item)}><Trash2 size={18} /></button></>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>
      {settingsOpen ? <AccountSettings session={session} onClose={() => setSettingsOpen(false)} onPurged={async () => { setSettingsOpen(false); setFolderStack([]); setData(await listFiles(session.accessToken)); }} onUpdated={(auth) => { onSessionUpdated(auth); setSettingsOpen(false); }} /> : null}
      {previewItem ? <FilePreview accessToken={session.accessToken} item={previewItem} onClose={() => setPreviewItem(null)} /> : null}
      {messagesOpen ? <MessagesPanel accessToken={session.accessToken} onClose={() => setMessagesOpen(false)} /> : null}
      {shareItem ? <ShareDialog accessToken={session.accessToken} item={shareItem} onClose={() => setShareItem(null)} /> : null}
      {nameDialog ? <NameDialog initialName={nameDialog.item?.name ?? ""} mode={nameDialog.mode} onClose={() => setNameDialog(null)} onSubmit={async (name) => { if (nameDialog.mode === "folder") await createFolder(session.accessToken, name, folderStack.at(-1)?.id); else if (nameDialog.item) await renameFile(session.accessToken, nameDialog.item.id, name); setNameDialog(null); await refresh(); }} /> : null}
    </main>
  );
}

function NameDialog({ initialName, mode, onClose, onSubmit }: { initialName: string; mode: "folder" | "rename"; onClose: () => void; onSubmit: (name: string) => Promise<void> }) {
  const [name, setName] = useState(initialName);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError(null);
    try { await onSubmit(name); } catch (err) { setError(err instanceof Error ? err.message : "Operation failed"); setBusy(false); }
  }
  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }}><section className="settings-dialog" role="dialog" aria-modal="true" aria-labelledby="name-title"><div className="dialog-heading"><div><h2 id="name-title">{mode === "folder" ? "New folder" : "Rename file"}</h2></div><button className="icon-button" type="button" title="Close" aria-label="Close" onClick={onClose}><X size={19} /></button></div><form className="auth-form" onSubmit={submit}><label><span>Name</span><div className="input-shell">{mode === "folder" ? <Folder size={18} /> : <File size={18} />}<input autoFocus maxLength={255} required value={name} onChange={(event) => setName(event.target.value)} /></div></label>{error ? <p className="message error">{error}</p> : null}<button className="primary-action" disabled={busy} type="submit">{busy ? <Loader2 className="spin" size={18} /> : mode === "folder" ? <FolderPlus size={18} /> : <Pencil size={18} />}<span>{busy ? "Saving..." : "Save"}</span></button></form></section></div>;
}

function ShareDialog({ accessToken, item, onClose }: { accessToken: string; item: FileItem; onClose: () => void }) {
  const [hours, setHours] = useState(24);
  const [maxDownloads, setMaxDownloads] = useState("");
  const [created, setCreated] = useState<CreatedShare | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const limit = maxDownloads ? Number(maxDownloads) : undefined;
      setCreated(await createShare(accessToken, item.id, hours * 3600, limit));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create link");
    } finally {
      setBusy(false);
    }
  }

  async function copyLink() {
    if (!created) return;
    await navigator.clipboard.writeText(created.url);
    setCopied(true);
  }

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }}>
      <section className="settings-dialog" role="dialog" aria-modal="true" aria-labelledby="share-title">
        <div className="dialog-heading"><div><h2 id="share-title">Share {item.name}</h2><p>Create a temporary public download link.</p></div><button className="icon-button" type="button" title="Close" aria-label="Close share dialog" onClick={onClose}><X size={19} /></button></div>
        {!created ? <form className="auth-form" onSubmit={submit}>
          <label><span>Expires after</span><select value={hours} onChange={(event) => setHours(Number(event.target.value))}><option value={1}>1 hour</option><option value={24}>1 day</option><option value={168}>7 days</option><option value={720}>30 days</option></select></label>
          <label><span>Maximum downloads</span><div className="input-shell"><Download size={18} /><input min={1} max={10000} placeholder="Unlimited" type="number" value={maxDownloads} onChange={(event) => setMaxDownloads(event.target.value)} /></div></label>
          {error ? <p className="message error" role="alert">{error}</p> : null}
          <button className="primary-action" disabled={busy} type="submit">{busy ? <Loader2 className="spin" size={18} /> : <Share2 size={18} />}<span>{busy ? "Creating..." : "Create link"}</span></button>
        </form> : <div className="share-result"><p>Link expires {formatDate(created.expiresAt)}</p><div className="share-link"><input readOnly value={created.url} /><button className="icon-button" type="button" title="Copy link" aria-label="Copy share link" onClick={() => void copyLink()}><Copy size={18} /></button></div>{copied ? <p className="message status">Copied to clipboard</p> : null}</div>}
      </section>
    </div>
  );
}

function MessagesPanel({ accessToken, onClose }: { accessToken: string; onClose: () => void }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [body, setBody] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refreshMessages = useCallback(async () => {
    try {
      setMessages(await listMessages(accessToken));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load messages");
    }
  }, [accessToken]);

  useEffect(() => {
    void refreshMessages();
  }, [refreshMessages]);

  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!body.trim()) return;
    setBusy(true);
    try {
      const created = await createMessage(accessToken, body);
      setMessages((current) => [created, ...current]);
      setBody("");
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to send message");
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    try {
      await deleteMessage(accessToken, id);
      setMessages((current) => current.filter((message) => message.id !== id));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete message");
    }
  }

  return (
    <div className="drawer-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }}>
      <aside className="messages-panel" role="dialog" aria-modal="true" aria-labelledby="messages-title">
        <div className="dialog-heading"><div><h2 id="messages-title">Messages</h2><p>Notes synced across your devices.</p></div><button className="icon-button" type="button" title="Close" aria-label="Close messages" onClick={onClose}><X size={19} /></button></div>
        <form className="message-compose" onSubmit={send}><textarea maxLength={4000} placeholder="Leave a message..." required value={body} onChange={(event) => setBody(event.target.value)} /><button className="icon-button" disabled={busy || !body.trim()} type="submit" title="Send" aria-label="Send message">{busy ? <Loader2 className="spin" size={18} /> : <Send size={18} />}</button></form>
        {error ? <p className="message error" role="alert">{error}</p> : null}
        <div className="messages-list">
          {messages.length === 0 ? <div className="empty-state"><MessageSquare size={30} /><p>No messages yet</p></div> : messages.map((message) => <article className="message-item" key={message.id}><p>{message.body}</p><footer><time>{formatDate(message.createdAt)}</time><button className="icon-button danger" type="button" title="Delete message" aria-label="Delete message" onClick={() => void remove(message.id)}><Trash2 size={16} /></button></footer></article>)}
        </div>
      </aside>
    </div>
  );
}

type FilePreviewProps = {
  accessToken: string;
  item: FileItem;
  onClose: () => void;
};

function FilePreview({ accessToken, item, onClose }: FilePreviewProps) {
  const [url, setURL] = useState<string | null>(null);
  const [textPreview, setTextPreview] = useState<string | null>(null);
  const [contentType, setContentType] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let objectURL: string | null = null;
    let cancelled = false;
    fetchPreview(accessToken, item.id)
      .then(async ({ blob, contentType: type }) => {
        if (cancelled) return;
        const mediaType = type.split(";", 1)[0];
        setContentType(mediaType);
        if (mediaType === "text/plain") {
          setTextPreview(await blob.text());
          return;
        }
        objectURL = URL.createObjectURL(blob);
        setURL(objectURL);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Preview failed");
      });
    return () => {
      cancelled = true;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [accessToken, item.id]);

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }}>
      <section className="preview-dialog" role="dialog" aria-modal="true" aria-labelledby="preview-title">
        <div className="dialog-heading"><div><h2 id="preview-title">{item.name}</h2><p>{formatBytes(item.sizeBytes)}</p></div><button className="icon-button" type="button" title="Close" aria-label="Close preview" onClick={onClose}><X size={19} /></button></div>
        <div className="preview-content">
          {error ? <div className="empty-state"><File size={32} /><p>{error}</p></div> : textPreview !== null ? <pre>{textPreview}</pre> : !url ? <Loader2 className="spin" size={28} aria-label="Loading preview" /> : contentType.startsWith("image/") ? <img src={url} alt={item.name} /> : contentType.startsWith("video/") ? <video src={url} controls /> : contentType.startsWith("audio/") ? <audio src={url} controls /> : contentType === "application/pdf" ? <iframe src={url} title={item.name} sandbox="" /> : null}
        </div>
      </section>
    </div>
  );
}

type AccountSettingsProps = {
  session: StoredSession;
  onClose: () => void;
  onPurged: () => Promise<void>;
  onUpdated: (auth: AuthResponse) => void;
};

function AccountSettings({ session, onClose, onPurged, onUpdated }: AccountSettingsProps) {
  const [loginValue, setLoginValue] = useState(session.user.login);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [purgeOpen, setPurgeOpen] = useState(false);
  const [purgePassword, setPurgePassword] = useState("");
  const [purgeConfirmation, setPurgeConfirmation] = useState("");
  const [purging, setPurging] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onUpdated(await updateCredentials(session.accessToken, currentPassword, loginValue, newPassword || undefined));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update account");
    } finally {
      setBusy(false);
    }
  }

  async function purge(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPurging(true);
    setError(null);
    try {
      await purgeStorage(session.accessToken, purgePassword, purgeConfirmation);
      await onPurged();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to clear storage");
    } finally {
      setPurging(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }}>
      <section className="settings-dialog" role="dialog" aria-modal="true" aria-labelledby="settings-title">
        <div className="dialog-heading">
          <div><h2 id="settings-title">Account settings</h2><p>Change your login or password.</p></div>
          <button className="icon-button" type="button" title="Close" aria-label="Close settings" onClick={onClose}><X size={19} /></button>
        </div>
        <form className="auth-form" onSubmit={submit}>
          <label><span>Login</span><div className="input-shell"><User size={18} /><input autoComplete="username" minLength={3} maxLength={32} required value={loginValue} onChange={(event) => setLoginValue(event.target.value)} /></div></label>
          <label><span>Current password</span><div className="input-shell"><Lock size={18} /><input autoComplete="current-password" minLength={12} required type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></div></label>
          <label><span>New password</span><div className="input-shell"><Lock size={18} /><input autoComplete="new-password" minLength={12} placeholder="Leave empty to keep current" type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></div></label>
          {error ? <p className="message error" role="alert">{error}</p> : null}
          <button className="primary-action" disabled={busy} type="submit">{busy ? <Loader2 className="spin" size={18} /> : <Settings size={18} />}<span>{busy ? "Saving..." : "Save changes"}</span></button>
        </form>
        <section className="danger-zone">
          <div><h3>Clear storage</h3><p>Permanently delete all your files, folders and share links.</p></div>
          {!purgeOpen ? <button className="danger-action" type="button" onClick={() => setPurgeOpen(true)}><Trash2 size={18} />Clear storage</button> : (
            <form className="auth-form" onSubmit={purge}>
              <label><span>Current password</span><div className="input-shell"><Lock size={18} /><input autoComplete="current-password" minLength={12} required type="password" value={purgePassword} onChange={(event) => setPurgePassword(event.target.value)} /></div></label>
              <label><span>Type DELETE ALL FILES</span><div className="input-shell"><Trash2 size={18} /><input autoComplete="off" required value={purgeConfirmation} onChange={(event) => setPurgeConfirmation(event.target.value)} /></div></label>
              <button className="danger-action" disabled={purging || purgeConfirmation !== "DELETE ALL FILES"} type="submit">{purging ? <Loader2 className="spin" size={18} /> : <Trash2 size={18} />}<span>{purging ? "Clearing..." : "Delete everything"}</span></button>
            </form>
          )}
        </section>
      </section>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}
