// Entry wiring: app context (repositories, index status, API base), the
// hash router, the shell (top bar + banners), workspace dispatch and a
// component error boundary so one broken workspace never takes down the app.
import { Component, useEffect } from 'react';
import type { ErrorInfo, ReactNode } from 'react';
import { AppProvider } from '@/lib/appContext';
import { navigate, useRoute } from '@/lib/router';
import { Shell } from '@/components/shell';
import { ErrorBlock } from '@/components/states';
import { ArchitectureWorkspace } from '@/workspaces/architect';
import { InspectorWorkspace } from '@/workspaces/inspect';
import { SearchWorkspace } from '@/workspaces/search';
import { HistoryWorkspace } from '@/workspaces/history';
import { ExplainerWorkspace } from '@/workspaces/explain';

const WORKSPACES: Array<{ name: string; component: () => ReactNode }> = [
  { name: 'architect', component: () => <ArchitectureWorkspace /> },
  { name: 'inspect', component: () => <InspectorWorkspace /> },
  { name: 'search', component: () => <SearchWorkspace /> },
  { name: 'history', component: () => <HistoryWorkspace /> },
  { name: 'explainer', component: () => <ExplainerWorkspace /> },
];

function UnknownRoute() {
  return (
    <div className="panel screen-enter" style={{ padding: 16 }}>
      <ErrorBlock
        title="Unknown screen"
        message="That hash does not match any workspace. The top-bar tabs take you back to one."
      />
    </div>
  );
}

class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null; info: ErrorInfo | null }> {
  state = { error: null as Error | null, info: null as ErrorInfo | null };

  static getDerivedStateFromError(error: Error, info: ErrorInfo) {
    return { error, info };
  }

  render() {
    if (this.state.error) {
      return (
        <div className="panel screen-enter" style={{ padding: 16 }}>
          <ErrorBlock
            title="This screen crashed"
            message={`${this.state.error.message}${this.state.info ? ` — ${String(this.state.info.message)}` : ''}. The rest of the app is unaffected; switch screens from the top bar.`}
            onRetry={() => this.setState({ error: null, info: null })}
          />
        </div>
      );
    }
    return this.props.children;
  }
}

function Root() {
  const route = useRoute();

  // Enter the app at its home screen when there is no hash yet.
  useEffect(() => {
    if (!window.location.hash || window.location.hash === '#/') navigate('architect');
  }, []);

  const match = WORKSPACES.find((w) => w.name === route.name);
  const content = match ? match.component() : route.raw ? <UnknownRoute /> : <ArchitectureWorkspace />;

  return (
    <Shell route={route}>
      <ErrorBoundary>
        {/* key re-mounts per route so each screen-enter animation and
            workspace-local state resets on navigation */}
        <div key={route.name} className="screen-enter" style={{ padding: '20px 0 48px' }}>
          {content}
        </div>
      </ErrorBoundary>
    </Shell>
  );
}

export function App() {
  return (
    <AppProvider>
      <Root />
    </AppProvider>
  );
}
