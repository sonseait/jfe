import React, { Component, type ErrorInfo, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { MantineProvider, createTheme, Button, Modal, ScrollArea } from '@mantine/core';
import { QueryClientProvider } from '@tanstack/react-query';
import { HashRouter, BrowserRouter } from 'react-router-dom';
import { Toaster } from 'react-hot-toast';
import '@mantine/core/styles.css';
import '@fontsource/be-vietnam-pro/latin-400.css';
import '@fontsource/be-vietnam-pro/latin-500.css';
import '@fontsource/be-vietnam-pro/latin-600.css';
import '@fontsource/be-vietnam-pro/latin-700.css';
import '@fontsource/be-vietnam-pro/vietnamese-400.css';
import '@fontsource/be-vietnam-pro/vietnamese-500.css';
import '@fontsource/be-vietnam-pro/vietnamese-600.css';
import '@fontsource/be-vietnam-pro/vietnamese-700.css';
import './styles/app.css';
import i18n from './locales';
import App from './native/App';
import { config, loadConfig } from './lib/config';
import { queryClient } from './lib/query-client';
const theme = createTheme({
  fontFamily: '"Be Vietnam Pro", sans-serif',
  headings: { fontFamily: '"Be Vietnam Pro", sans-serif', fontWeight: '600' },
  primaryColor: 'cinema',
  colors: {
    cinema: [
      '#fff4e8',
      '#ffe7cd',
      '#ffd0a1',
      '#f9b87b',
      '#eea35e',
      '#e99448',
      '#e48a3e',
      '#ca7530',
      '#b56727',
      '#9e571d',
    ],
    dark: [
      '#e9e9e4',
      '#c2c2ba',
      '#9c9c94',
      '#70716a',
      '#3d3e38',
      '#2b2c27',
      '#22231f',
      '#191a17',
      '#141512',
      '#10110e',
    ],
  },
  defaultRadius: 'md',
  cursorType: 'pointer',
  components: {
    Button: { defaultProps: { fw: 600 } },
    Modal: Modal.extend({
      defaultProps: {
        overlayProps: { backgroundOpacity: 0.65, blur: 8 },
        scrollAreaComponent: ScrollArea.Autosize,
      },
    }),
  },
});
class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(_error: Error, _info: ErrorInfo) {
    /* Do not log potentially sensitive server responses. */
  }
  render() {
    return this.state.failed ? (
      <div className="empty">
        <h1>{i18n.t('error')}</h1>
        <Button onClick={() => window.location.reload()}>{i18n.t('retry')}</Button>
      </div>
    ) : (
      this.props.children
    );
  }
}
async function bootstrap() {
  await loadConfig();
  const Router = config.routerMode === 'history' ? BrowserRouter : HashRouter;
  createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <MantineProvider theme={theme} forceColorScheme="dark">
        <ErrorBoundary>
          <QueryClientProvider client={queryClient}>
            <Router
              basename={config.routerMode === 'history' ? import.meta.env.BASE_URL : undefined}
            >
              <App />
            </Router>
            <Toaster
              position="top-right"
              toastOptions={{
                style: { background: '#292b25', color: '#f2f0e8', border: '1px solid #42443c' },
              }}
            />
          </QueryClientProvider>
        </ErrorBoundary>
      </MantineProvider>
    </React.StrictMode>,
  );
}
void bootstrap().catch(() => {
  const root = document.getElementById('root')!;
  root.textContent = i18n.t('error');
  const button = document.createElement('button');
  button.textContent = i18n.t('retry');
  button.onclick = () => window.location.reload();
  root.append(button);
});
