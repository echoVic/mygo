/** How a page posts to Go: WebKit's message handler, or WebView2's. */
export interface Transport {
  post(message: string): void;
  /** Posts File objects along with a message (WebView2). */
  postWithFiles?(message: string, files: FileList): void;
}

/** The page's way to Go, or null outside a MyGo window. */
export function transport(w: any): Transport | null {
  const handler = w.webkit?.messageHandlers?.mygo;
  if (handler) return { post: (m) => handler.postMessage(m) };
  const webview = w.chrome?.webview;
  if (!webview) return null;
  return {
    post: (m) => webview.postMessage(m),
    postWithFiles: webview.postMessageWithAdditionalObjects
      ? (m, files) => webview.postMessageWithAdditionalObjects(m, files)
      : undefined,
  };
}
