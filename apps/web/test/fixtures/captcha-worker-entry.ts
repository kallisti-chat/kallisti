// exercise vite's actual worker bundling, not a mocked worker.
new Worker(new URL("../../src/lib/captcha.worker.ts", import.meta.url), { type: "module" });
