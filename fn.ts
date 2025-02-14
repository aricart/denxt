import type { Msg } from "jsr:@nats-io/transport-deno";

export function fn(_: Error, msg: Msg): void {
  msg.respond(msg.data);
}
