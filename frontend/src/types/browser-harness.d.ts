declare module '*browserHarness.mjs' {
  import type { IncomingMessage, ServerResponse } from 'node:http'

  export const browserTestsEnabled: boolean

  // Playwright page methods this harness returns. evaluate stays loosely typed:
  // the tests pass page expressions as strings.
  export interface BrowserPage {
    errors: unknown[]
    evaluate(expression: string): Promise<any>
    waitForFunction(expression: string): Promise<unknown>
    click(selector: string, options?: { position?: { x: number; y: number } }): Promise<void>
    keyboard: { press(key: string): Promise<void> }
    screenshot(): Promise<Buffer>
    setViewportSize(viewportSize: { width: number; height: number }): Promise<void>
    locator(selector: string): {
      boundingBox(): Promise<{ x: number; y: number; width: number; height: number } | null>
    }
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
