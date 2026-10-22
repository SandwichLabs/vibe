export const DEV_ORIGINS: string[]
export function studioOrigins(command: 'serve' | 'build', env?: Record<string, string | undefined>, platform?: string): string[]
export function frameCspMeta(origins: string[]): string
export function injectFrameCsp(html: string, origins: string[]): string
