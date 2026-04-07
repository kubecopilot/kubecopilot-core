import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import {
  Page,
  PageSection,
  Title,
  EmptyState,
  EmptyStateBody,
  Spinner,
} from '@patternfly/react-core';
import { useCallback, useEffect, useRef, useState } from 'react';
import './KubeCopilotPage.css';

const PLUGIN_CONFIG_URL = '/api/plugins/kube-copilot-console-plugin/plugin-config.json';

/** Find the console main content area element. */
function findMainElement(): HTMLElement | null {
  return (
    (document.querySelector('.pf-v5-c-page__main') as HTMLElement) ||
    (document.querySelector('main') as HTMLElement)
  );
}

export default function KubeCopilotPage() {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [webUIUrl, setWebUIUrl] = useState<string | null>(null);
  const [configError, setConfigError] = useState(false);
  const [loading, setLoading] = useState(true);
  const [iframeError, setIframeError] = useState(false);
  const [ready, setReady] = useState(false);

  // Position the fixed wrapper to exactly cover the console main content area
  const adjust = useCallback(() => {
    const el = wrapperRef.current;
    if (!el) return;
    const main = findMainElement();
    if (!main) return;
    const r = main.getBoundingClientRect();
    el.style.top = `${r.top}px`;
    el.style.left = `${r.left}px`;
    el.style.width = `${r.width}px`;
    el.style.height = `${r.height}px`;
    el.style.right = 'unset';
    el.style.bottom = 'unset';
    setReady((prev) => (prev ? prev : true));
  }, []);

  // Measure the actual page main content area and position the wrapper to match it exactly
  useEffect(() => {
    let retryCount = 0;
    const MAX_RETRIES = 20;
    let retryId: ReturnType<typeof setTimeout> | undefined;
    let rafId: ReturnType<typeof requestAnimationFrame> | undefined;
    let ro: ResizeObserver | undefined;

    // Attach ResizeObserver to the main content area once it is available
    const attachResizeObserver = (main: HTMLElement) => {
      if (ro || typeof ResizeObserver === 'undefined') return;
      ro = new ResizeObserver(adjust);
      ro.observe(main);
    };

    // Initial positioning (with bounded retry for late-rendering elements)
    const tryAdjust = () => {
      adjust();
      const main = findMainElement();
      if (main) {
        attachResizeObserver(main);
      } else if (retryCount < MAX_RETRIES) {
        retryCount++;
        retryId = setTimeout(tryAdjust, 100);
      }
    };
    rafId = requestAnimationFrame(tryAdjust);

    // Re-adjust on viewport resize
    window.addEventListener('resize', adjust);

    // Watch sidebar and page elements for class/style mutations (e.g. sidebar toggle)
    const mo = new MutationObserver(adjust);
    document.querySelectorAll('.pf-v5-c-page, .pf-v5-c-page__sidebar').forEach((node) =>
      mo.observe(node, {
        attributes: true,
        childList: false,
        subtree: false,
        attributeFilter: ['class', 'style'],
      }),
    );

    return () => {
      if (rafId !== undefined) cancelAnimationFrame(rafId);
      if (retryId !== undefined) clearTimeout(retryId);
      window.removeEventListener('resize', adjust);
      ro?.disconnect();
      mo.disconnect();
    };
  }, [adjust]);

  // Fetch plugin config to get the web-ui Route URL
  useEffect(() => {
    fetch(PLUGIN_CONFIG_URL)
      .then((r) => r.json())
      .then((cfg) => {
        if (cfg?.webUIUrl) {
          setWebUIUrl(cfg.webUIUrl);
        } else {
          setConfigError(true);
          setLoading(false);
        }
      })
      .catch(() => {
        setConfigError(true);
        setLoading(false);
      });
  }, []);

  // Attach iframe load/error handlers once URL is resolved
  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe || !webUIUrl) return;

    const handleLoad = () => setLoading(false);
    const handleError = () => {
      setLoading(false);
      setIframeError(true);
    };

    iframe.addEventListener('load', handleLoad);
    iframe.addEventListener('error', handleError);
    return () => {
      iframe.removeEventListener('load', handleLoad);
      iframe.removeEventListener('error', handleError);
    };
  }, [webUIUrl]);

  // Forward theme changes into the iframe
  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;

    const observer = new MutationObserver(() => {
      const theme = document.documentElement.classList.contains('pf-v5-theme-dark')
        ? 'dark'
        : 'light';
      iframe.contentWindow?.postMessage({ type: 'theme-change', theme }, '*');
    });

    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    });
    return () => observer.disconnect();
  }, []);

  if (configError) {
    return (
      <>
        <DocumentTitle>KubeCopilot AI</DocumentTitle>
        <Page>
          <PageSection>
            <EmptyState>
              <Title headingLevel="h4" size="lg">KubeCopilot URL not configured</Title>
              <EmptyStateBody>
                Could not load plugin configuration. Ensure the Helm chart was deployed
                with <code>webUI.routeUrl</code> set to the KubeCopilot Web UI route URL.
              </EmptyStateBody>
            </EmptyState>
          </PageSection>
        </Page>
      </>
    );
  }

  const iframeSrc = webUIUrl
    ? (webUIUrl.includes('?') ? `${webUIUrl}&embedded=true` : `${webUIUrl}?embedded=true`)
    : '';

  return (
    <>
      <DocumentTitle>KubeCopilot AI</DocumentTitle>
      <div
        className={`kube-copilot-plugin${ready ? ' kube-copilot-plugin--ready' : ''}`}
        ref={wrapperRef}
      >
        {loading && (
          <div className="kube-copilot-plugin__loading">
            <Spinner size="xl" />
            <p>Loading KubeCopilot...</p>
          </div>
        )}
        {iframeError && (
          <div className="kube-copilot-plugin__error">
            <EmptyState>
              <Title headingLevel="h4" size="lg">Unable to load KubeCopilot</Title>
              <EmptyStateBody>
                Could not connect to the KubeCopilot Web UI at{' '}
                <code>{webUIUrl}</code>. Please check that the service is running.
              </EmptyStateBody>
            </EmptyState>
          </div>
        )}
        {webUIUrl && (
          <iframe
            ref={iframeRef}
            src={iframeSrc}
            className="kube-copilot-plugin__iframe"
            title="KubeCopilot AI Agent"
            allow="clipboard-read; clipboard-write"
            sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals"
            style={{ display: loading || iframeError ? 'none' : 'block' }}
          />
        )}
      </div>
    </>
  );
}
