export interface RuntimeConfig {
  routerMode: 'hash' | 'history';
}
export const config: RuntimeConfig = {
  routerMode: 'hash',
};
export async function loadConfig() {
  const response = await fetch(`${import.meta.env.BASE_URL}config.json`);
  if (!response.ok) throw new Error('Unable to load config.json');
  const value: Partial<RuntimeConfig> = await response.json();
  config.routerMode = value.routerMode === 'history' ? 'history' : 'hash';
}
