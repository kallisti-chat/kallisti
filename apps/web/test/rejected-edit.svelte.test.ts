// @vitest-environment jsdom
import { flushSync } from "svelte";
import { expect, it, vi } from "vitest";
import { edit, flush, input, show, submit } from "./helpers/composer.svelte";

it("keeps a rejected edit and attachment choices until acceptance", async () => {
  let reject!: (e: Error) => void, resolve!: () => void;
  const onEdit = vi.fn().mockImplementationOnce(() => new Promise<void>((_, no) => { reject = no; })).mockImplementationOnce(() => new Promise<void>((yes) => { resolve = yes; }));
  show({ onEdit }); edit(1);
  input(document.querySelector("form.edit textarea")!, "proposed");
  document.querySelector<HTMLButtonElement>(".edit-files button")!.click(); flushSync();
  submit("form.edit");
  expect(document.querySelector("form.edit textarea")).not.toBeNull();
  submit("form.edit"); expect(onEdit).toHaveBeenCalledTimes(1);
  expect(onEdit).toHaveBeenCalledWith(1, "proposed", ["key"]);
  reject(new Error("refused")); await flush();
  expect(document.querySelector<HTMLTextAreaElement>("form.edit textarea")!.value).toBe("proposed");
  expect(document.querySelector(".edit-files li.gone")).not.toBeNull();
  expect(document.querySelector('form.edit [role="alert"]')!.textContent).toBe("refused");
  submit("form.edit"); resolve(); await flush();
  expect(document.querySelector("form.edit")).toBeNull();
});

it("does not close another edit when an old request settles", async () => {
  let resolve!: () => void;
  show({ onEdit: () => new Promise<void>((yes) => { resolve = yes; }) }); edit(1);
  input(document.querySelector("form.edit textarea")!, "first"); submit("form.edit");
  edit(2); input(document.querySelector("form.edit textarea")!, "second");
  resolve(); await flush();
  expect(document.querySelector<HTMLTextAreaElement>('[data-message="2"] form.edit textarea')!.value).toBe("second");
});

it("keeps a rejected inactive edit separate and restores it when reopened", async () => {
  let reject!: (e: Error) => void;
  show({ onEdit: () => new Promise<void>((_, no) => { reject = no; }) }); edit(1);
  input(document.querySelector("form.edit textarea")!, "first proposed");
  document.querySelector<HTMLButtonElement>(".edit-files button")!.click(); submit("form.edit");
  edit(2); input(document.querySelector("form.edit textarea")!, "second proposed");
  reject(new Error("refused")); await flush();
  expect(document.querySelector<HTMLTextAreaElement>("form.edit textarea")!.value).toBe("second proposed");
  edit(1); await flush();
  expect(document.querySelector<HTMLTextAreaElement>("form.edit textarea")!.value).toBe("first proposed");
  expect(document.querySelector(".edit-files li.gone")).not.toBeNull();
});

it.each(["cancel", "no-change"] as const)("discards a rejected draft on %s instead of restoring it next time", async (exit) => {
  const onEdit = vi.fn(async () => { throw new Error("refused"); });
  show({ onEdit }); edit(1);
  input(document.querySelector("form.edit textarea")!, "cancelled proposal");
  document.querySelector<HTMLButtonElement>(".edit-files button")!.click();
  submit("form.edit"); await flush();
  expect(document.querySelector(".edit-files li.gone")).not.toBeNull();
  expect(document.querySelector('form.edit [role="alert"]')!.textContent).toBe("refused");

  if (exit === "cancel") {
    document.querySelector("form.edit textarea")!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  } else {
    input(document.querySelector("form.edit textarea")!, "original");
    document.querySelector<HTMLButtonElement>(".edit-files button")!.click();
    await flush();
    submit("form.edit");
  }
  await flush();
  expect(document.querySelector("form.edit")).toBeNull();
  expect(onEdit).toHaveBeenCalledTimes(1);

  edit(1); await flush();
  expect(document.querySelector<HTMLTextAreaElement>("form.edit textarea")!.value).toBe("original");
  expect(document.querySelector(".edit-files li.gone")).toBeNull();
  expect(document.querySelector('form.edit [role="alert"]')).toBeNull();
});
