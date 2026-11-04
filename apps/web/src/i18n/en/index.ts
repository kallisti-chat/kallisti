// English: the source texts. Keys are "area.name"; each area has a file.
import common from "./common";
import chat from "./chat";
import client from "./client";
import settings from "./settings";
import admin from "./admin";
import login from "./login";
import sidebar from "./sidebar";
import media from "./media";

export const en = { ...common, ...chat, ...client, ...settings, ...admin, ...login, ...sidebar, ...media } as const;
export type Key = keyof typeof en;
export default en;
