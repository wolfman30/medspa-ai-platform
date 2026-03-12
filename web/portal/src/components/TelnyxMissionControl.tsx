import { useEffect, useState } from 'react';

const TELNYX_URL = 'https://portal.telnyx.com/';
const FALLBACK_DELAY_MS = 6000;

export function TelnyxMissionControl() {
  const [iframeLoaded, setIframeLoaded] = useState(false);
  const [showFallback, setShowFallback] = useState(false);

  useEffect(() => {
    if (iframeLoaded) return;

    const fallbackTimer = window.setTimeout(() => {
      setShowFallback(true);
    }, FALLBACK_DELAY_MS);

    return () => window.clearTimeout(fallbackTimer);
  }, [iframeLoaded]);

  return (
    <div className="ui-page px-4 sm:px-6 lg:px-8 py-6">
      <div className="mb-4 rounded-2xl border border-slate-700/70 bg-slate-900/90 px-5 py-4 shadow-lg shadow-black/25">
        <p className="text-sm font-medium text-slate-200">
          Telnyx Mission Control — manage numbers, messaging profiles, and call control
        </p>
      </div>

      {showFallback && !iframeLoaded ? (
        <div className="flex min-h-[calc(100vh-16rem)] items-center justify-center rounded-2xl border border-slate-700/70 bg-slate-900/85 p-8 text-center shadow-lg shadow-black/25">
          <div className="max-w-xl space-y-4">
            <p className="text-sm text-slate-300">
              Telnyx Mission Control can&apos;t be embedded in this portal due to browser iframe security restrictions.
            </p>
            <a
              href={TELNYX_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="ui-btn ui-btn-primary"
            >
              Open in new tab
            </a>
          </div>
        </div>
      ) : (
        <div className="relative h-[calc(100vh-16rem)] min-h-[640px] w-full overflow-hidden rounded-2xl border border-slate-700/70 bg-slate-950 shadow-xl shadow-black/30">
          {!iframeLoaded && (
            <div className="absolute inset-0 z-10 flex items-center justify-center bg-slate-950/90">
              <span className="text-sm text-slate-300">Loading Telnyx Mission Control…</span>
            </div>
          )}

          <iframe
            title="Telnyx Mission Control"
            src={TELNYX_URL}
            className="h-full w-full border-0"
            allow="clipboard-read; clipboard-write"
            onLoad={() => setIframeLoaded(true)}
            onError={() => setShowFallback(true)}
          />
        </div>
      )}
    </div>
  );
}
