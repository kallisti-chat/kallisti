export class Socket {
  static OPEN = 1;
  static latest: Socket;
  readyState = 1;
  binaryType = "";
  sent: string[] = [];
  onmessage?: (event: { data: string }) => void;
  onclose?: (event: { reason: string }) => void;
  constructor() { Socket.latest = this; }
  send(data: string) { this.sent.push(data); }
  close() { this.onclose?.({ reason: "" }); }
  reply(data: object) { this.onmessage?.({ data: JSON.stringify(data) }); }
}
