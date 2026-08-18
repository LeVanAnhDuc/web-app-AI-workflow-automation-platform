import { expect, test, type Page } from "@playwright/test";

/**
 * The one journey that has to work: sign in, build a two-node workflow on the
 * canvas, connect it, run it, and watch the execution log go green. It crosses
 * every process — Next, the Go API, the queue, the worker, the engine — so it
 * catches an integration that drifted, which no unit test can.
 *
 * Requires Postgres, cmd/api and cmd/worker to be running; see the README.
 */

const EMAIL = process.env.E2E_EMAIL ?? "ha.nguyen@acme.vn";
const PASSWORD = process.env.E2E_PASSWORD ?? "flowgrid123";

async function signIn(page: Page) {
  await page.goto("/login");

  // A session left by an earlier test sends us straight through, and then there
  // is no form to fill.
  if (new URL(page.url()).pathname === "/login") {
    await page.getByRole("textbox", { name: "Email" }).fill(EMAIL);
    await page.locator('input[name="password"]').fill(PASSWORD);
    await page.getByRole("button", { name: "Sign in" }).click();
  }
  await expect(page).toHaveURL(/\/workflows/);
}

/**
 * Drags between two nodes' handles. React Flow only latches onto a real pointer
 * sequence, so this needs intermediate moves — a single dragAndDrop does not
 * produce a connection.
 */
async function connect(page: Page, sourceNode: string, targetNode: string) {
  const source = page
    .locator(`.react-flow__node:has-text("${sourceNode}") .react-flow__handle.source`)
    .first();
  const target = page
    .locator(`.react-flow__node:has-text("${targetNode}") .react-flow__handle.target`)
    .first();

  const from = await source.boundingBox();
  const to = await target.boundingBox();
  if (!from || !to) throw new Error(`no handles for ${sourceNode} -> ${targetNode}`);

  const start = { x: from.x + from.width / 2, y: from.y + from.height / 2 };
  const end = { x: to.x + to.width / 2, y: to.y + to.height / 2 };

  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move((start.x + end.x) / 2, (start.y + end.y) / 2, { steps: 10 });
  await page.mouse.move(end.x, end.y, { steps: 10 });
  await page.mouse.up();
}

test("build a workflow on the canvas, run it, and see the log go green", async ({ page }) => {
  await signIn(page);

  await page.getByRole("button", { name: /new workflow/i }).click();
  await expect(page).toHaveURL(/\/workflows\/[0-9a-f-]{36}/);

  // Two nodes from the palette: a trigger, and something for it to feed.
  await page.getByRole("button", { name: "Manual Trigger", exact: true }).click();
  await expect(page.locator('.react-flow__node:has-text("Manual")')).toBeVisible();

  await page.getByRole("button", { name: "Set", exact: true }).click();
  await expect(page.locator('.react-flow__node:has-text("Set")')).toBeVisible();

  await connect(page, "Manual", "Set");
  await expect(page.locator(".react-flow__edge")).toHaveCount(1);

  // Fill the Set node's required Fields row, or the run is refused as invalid.
  // With no rows yet the drawer shows an empty-state prompt instead of the
  // "add a row" button, so accept either.
  await page.locator('.react-flow__node:has-text("Set")').click();
  const drawer = page.getByRole("complementary", { name: /configuration for/i });
  await expect(drawer).toBeVisible();

  await drawer
    .getByRole("button", { name: /add a fields row|no fields yet/i })
    .first()
    .click();
  await drawer.getByRole("textbox", { name: "Fields name 1" }).fill("stage");
  await drawer.getByRole("textbox", { name: "Fields value 1" }).fill("e2e");

  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Saved", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Run", exact: true }).click();

  // A green execution here proves the API recorded the run, the worker claimed
  // it off the Postgres queue, and the engine executed both nodes.
  await expect(page.getByText("Succeeded").first()).toBeVisible({ timeout: 40_000 });

  // Both nodes reported into the log panel.
  await expect(page.getByText("trigger.manual").last()).toBeVisible();
  await expect(page.getByText("set", { exact: true }).last()).toBeVisible();
});

test("the run shows up on the executions screen and its detail opens", async ({ page }) => {
  await signIn(page);

  await page.getByRole("link", { name: "Executions" }).click();
  await expect(page).toHaveURL(/\/executions/);

  const firstRow = page.locator("tbody tr, [data-row-id]").first();
  const clickable = (await firstRow.count())
    ? firstRow
    : page.getByText(/[0-9a-f]{8}-[0-9a-f]{4}/).first();
  await expect(clickable).toBeVisible({ timeout: 20_000 });
  await clickable.click();

  await expect(page).toHaveURL(/\/executions\/[0-9a-f-]{36}/);

  // The detail screen's two load-bearing pieces: the read-only replay canvas
  // and the data viewer beside it.
  await expect(page.locator(".react-flow")).toBeVisible();
  await expect(page.getByText("Input").first()).toBeVisible();
});
