import type { ListenOnly, Speaker } from "./listenOnly";
import type { Member } from "./privateChannels";

// What the channel dialog does: create, edit or delete a channel or a
// text category. Private channels (built-in server) for admins and
// moderators: canPrivate on create; private on edit (with the members,
// which stay). A voice channel made in another (parent) is a subchannel.
export type ChannelKind = "text" | "voice" | "category";
// A text channel's category (0: none); categories: the choice (staff).
type CategoryField = { category?: number; categories?: { id: number; name: string }[] };
export type ChannelForm =
  | ({ mode: "create"; kind: ChannelKind; temporaryOnly: boolean; canPrivate?: boolean; canCategory?: boolean; parent?: number } & CategoryField)
  | ({
      mode: "edit";
      kind: ChannelKind;
      name: string;
      description: string;
      // Voice channels with the built-in server; users: who may become a
      // speaker.
      listenOnly?: ListenOnly & { users: Speaker[] };
      // Text channels with a mirror (staff): the same for who may post.
      readOnly?: ListenOnly & { users: Speaker[] };
      private?: { on: boolean; members: Member[] };
      // Text channels with the built-in server: seconds (see lib/slowMode).
      slowMode?: number;
      // A text channel's moderator: only the topic and slow mode change.
      topicOnly?: boolean;
    } & CategoryField)
  | { mode: "delete"; kind: ChannelKind; name: string };
