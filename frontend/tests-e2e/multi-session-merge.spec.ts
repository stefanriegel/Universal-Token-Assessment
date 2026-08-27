/**
 * multi-session-merge.spec.ts
 *
 * End-to-end verification of multi-session-file merging against three real
 * session files exported by the shipped tool (v3.9.0):
 *   ddi-session-2026-07-17.json      -> azure,     30 findings
 *   ddi-session-2026-07-17(1).json   -> aws,      219 findings
 *   ddi-session-2026-07-17 (2).json  -> microsoft,  4 findings
 *
 * Expected merged sizing, established independently against mergeSnapshots +
 * calcUddiTokensAggregated: 253 findings, 1,372 management tokens, 0 conflicts.
 * Removing the aws file leaves azure+microsoft: 34 findings, 673 tokens.
 *
 * The merged total (1,372) is deliberately LOWER than the sum of the three
 * files sized separately (23 + 700 + 652 = 1,375). That is the point of a
 * unified assessment: counts are pooled per category and the ceiling division
 * is applied once, instead of once per file.
 */

import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

const APP_URL = process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:5173';
const DIR = '/Users/mustermann/Downloads/tmp';

const AZURE_FILE = `${DIR}/ddi-session-2026-07-17.json`;
const AWS_FILE = `${DIR}/ddi-session-2026-07-17(1).json`;
const AD_FILE = `${DIR}/ddi-session-2026-07-17 (2).json`;

async function loadFiles(page: Page, files: string[]) {
  await page.goto(APP_URL);
  await page.getByTestId('session-file-input').setInputFiles(files);
}

test('three session files merge into one unified sizing', async ({ page }) => {
  await loadFiles(page, [AZURE_FILE, AWS_FILE, AD_FILE]);

  // All three files listed, with their remove controls.
  for (const name of [
    'ddi-session-2026-07-17.json',
    'ddi-session-2026-07-17(1).json',
    'ddi-session-2026-07-17 (2).json',
  ]) {
    await expect(page.getByRole('button', { name: new RegExp(`Remove ${name.replace(/[.()[\]]/g, '\\$&')}`) }))
      .toBeVisible();
  }

  // Distinct providers, so no conflicts.
  await expect(page.getByText(/Merged with \d+ conflict/)).toHaveCount(0);

  // The drop zone is still mounted after merging — this is the regression
  // guard for the unmount bug that made the whole list unreachable.
  await expect(page.getByTestId('session-file-input')).toBeAttached();

  // Continue to results and check the unified total.
  await page.getByRole('button', { name: /continue|next/i }).first().click();
  await expect(page.locator('body')).toContainText('1,372', { timeout: 10_000 });
});

test('removing a file recomputes the sizing', async ({ page }) => {
  await loadFiles(page, [AZURE_FILE, AWS_FILE, AD_FILE]);

  await page
    .getByRole('button', { name: /Remove ddi-session-2026-07-17\(1\)\.json/ })
    .click();

  // The aws row is gone, the other two remain.
  await expect(
    page.getByRole('button', { name: /Remove ddi-session-2026-07-17\(1\)\.json/ })
  ).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: /Remove ddi-session-2026-07-17\.json/ })
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: /Remove ddi-session-2026-07-17 \(2\)\.json/ })
  ).toBeVisible();

  await page.getByRole('button', { name: /continue|next/i }).first().click();
  await expect(page.locator('body')).toContainText('673', { timeout: 10_000 });
});

test('a corrupt file is reported without blocking the valid ones', async ({ page }) => {
  await page.goto(APP_URL);
  // Playwright cannot mix paths and buffers in one call, so pass both as buffers.
  await page.getByTestId('session-file-input').setInputFiles([
    { name: 'broken.json', mimeType: 'application/json', buffer: Buffer.from('not json at all') },
    {
      name: 'ddi-session-2026-07-17.json',
      mimeType: 'application/json',
      buffer: readFileSync(AZURE_FILE),
    },
  ]);

  await expect(page.getByText(/broken\.json/)).toBeVisible();
  await expect(
    page.getByRole('button', { name: /Remove ddi-session-2026-07-17\.json/ })
  ).toBeVisible();
});
