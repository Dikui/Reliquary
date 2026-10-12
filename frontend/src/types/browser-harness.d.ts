declare module '*browserHarness.mjs' {
  import type { IncomingMessage, ServerResponse } from 'node:http'

  export const chromeExecutable: string | undefined
  export const browserAvailable: boolean

  export interface BrowserPage {
    errors: unknown[]
    evaluate(expression: string): Promise<any>
    waitFor(expression: string, description?: string): Promise<any>
    click(selector: string, position?: { x: number; y: number }): Promise<void>
    pressEscape(): Promise<void>
    screenshot(): Promise<Buffer>
    viewport(width: number, height?: number): Promise<void>
  }

  export interface BrowserHarness {
    page(query?: string): Promise<BrowserPage>
    close(): Promise<void>
  }

  export function createBrowserHarness(
    clientSource: string,
    options?: { routes?: (request: IncomingMessage, response: ServerResponse) => boolean },
  ): Promise<BrowserHarness>
}
