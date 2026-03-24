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

/**
 * Resolves the KubeCopilot Web UI service URL.
 *
 * Priority:
 * 1. window.SERVER_FLAGS?.kubeCopilotUrl (injected via ConsolePlugin proxy)
 * 2. Meta tag <meta name="kube-copilot-url" ...>
 * 3. Fallback: service DNS inside cluster
 */
function resolveWebUIUrl(): string {
  // Check for meta tag override (useful for development)
  const metaEl = document.querySelector('meta[name="kube-copilot-url"]');
  if (metaEl) {
    return metaEl.getAttribute('content') || '';
  }

  // Default: use the in-cluster service URL via OpenShift proxy
  // The Helm chart values.yaml allows configuring this
  return '/api/proxy/plugin/kube-copilot-console-plugin/web-ui/';
}

export default function KubeCopilotPage() {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const webUIUrl = resolveWebUIUrl();

  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;

    const handleLoad = () => setLoading(false);
    const handleError = () => {
      setLoading(false);
      setError(true);
    };

    iframe.addEventListener('load', handleLoad);
    iframe.addEventListener('error', handleError);

    return () => {
      iframe.removeEventListener('load', handleLoad);
      iframe.removeEventListener('error', handleError);
    };
  }, []);

  // Listen for theme changes and forward to the iframe
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

  if (!webUIUrl) {
    return (
      <>
        <DocumentTitle>KubeCopilot AI</DocumentTitle>
        <Page>
          <PageSection>
            <EmptyState>
              <Title headingLevel="h4" size="lg">
                KubeCopilot URL not configured
              </Title>
              <EmptyStateBody>
                The KubeCopilot Web UI URL is not set. Please configure the
                plugin with the correct Web UI service URL.
              </EmptyStateBody>
            </EmptyState>
          </PageSection>
        </Page>
      </>
    );
  }

  const iframeSrc = webUIUrl.includes('?')
    ? `${webUIUrl}&embedded=true`
    : `${webUIUrl}?embedded=true`;

  return (
    <>
      <DocumentTitle>KubeCopilot AI</DocumentTitle>
      <div className="kube-copilot-plugin">
        {loading && (
          <div className="kube-copilot-plugin__loading">
            <Spinner size="xl" />
            <p>Loading KubeCopilot...</p>
          </div>
        )}
        {error && (
          <div className="kube-copilot-plugin__error">
            <EmptyState>
              <Title headingLevel="h4" size="lg">
                Unable to load KubeCopilot
              </Title>
              <EmptyStateBody>
                Could not connect to the KubeCopilot Web UI service. Please check
                that the service is running and accessible.
              </EmptyStateBody>
            </EmptyState>
          </div>
        )}
        <iframe
          ref={iframeRef}
          src={iframeSrc}
          className="kube-copilot-plugin__iframe"
          title="KubeCopilot AI Agent"
          allow="clipboard-read; clipboard-write"
          sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals"
          style={{ display: loading || error ? 'none' : 'block' }}
        />
      </div>
    </>
  );
}
