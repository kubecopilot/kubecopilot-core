import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import {
  Page,
  PageSection,
  Title,
  EmptyState,
  EmptyStateBody,
  Spinner,
} from '@patternfly/react-core';
import { useEffect, useRef, useState } from 'react';
import './KubeCopilotPage.css';

const PLUGIN_CONFIG_URL = '/api/plugins/kube-copilot-console-plugin/plugin-config.json';

export default function KubeCopilotPage() {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [webUIUrl, setWebUIUrl] = useState<string | null>(null);
  const [configError, setConfigError] = useState(false);
  const [loading, setLoading] = useState(true);
  const [iframeError, setIframeError] = useState(false);

  // Measure the actual page main content area and position the wrapper to match it exactly
  useEffect(() => {
    const adjust = () => {
      const el = wrapperRef.current;
      if (!el) return;
      // The console renders plugin page content inside .pf-v5-c-page__main
      const main =
        document.querySelector('.pf-v5-c-page__main') as HTMLElement ||
        document.querySelector('main') as HTMLElement;
      if (main) {
        const r = main.getBoundingClientRect();
        el.style.top    = `${r.top}px`;
        el.style.left   = `${r.left}px`;
        el.style.width  = `${r.width}px`;
        el.style.height = `${r.height}px`;
        el.style.right  = 'unset';
        el.style.bottom = 'unset';
      }
    };

    // Run immediately and on any layout change
    adjust();
    window.addEventListener('resize', adjust);
    const mo = new MutationObserver(adjust);
    document.querySelectorAll('.pf-v5-c-page, .pf-v5-c-page__sidebar').forEach(node =>
      mo.observe(node, { attributes: true, childList: false, subtree: false, attributeFilter: ['class', 'style'] })
    );
    return () => {
      window.removeEventListener('resize', adjust);
      mo.disconnect();
    };
  }, []);

  // Fetch plugin config to get the web-ui Route URL
  useEffect(() => {
    fetch(PLUGIN_CONFIG_URL)
      .then(r => r.json())
      .then(cfg => {
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
      <div className="kube-copilot-plugin" ref={wrapperRef}>
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
