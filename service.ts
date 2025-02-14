import {
  connect,
  type Msg,
  MsgCallback,
} from "jsr:@nats-io/transport-deno@3.0.0-22";

// so for this the easiest way is really to rename their source in a build dir
// to fn.ts, and then copy it there, and import it

import { fn } from "./fn.ts";

if (Deno.args.length !== 2) {
  console.error("Usage: service <host:port> <subject>");
  Deno.exit(1);
}

const subj = Deno.args[1];
const nc = await connect({ servers: [Deno.args[0]] });
nc.closed().then((err?) => {
  if (err) {
    console.error(err);
    Deno.exit(1);
  }
  Deno.exit(0);
});
console.log("connected", nc.getServer());

// all the runner to know we are up

const stop = nc.subscribe(`stop.${subj}`, {
  callback: (err, msg) => {
    if (err) {
      console.error(err);
    }
    nc.drain().then(() => Deno.exit(0));
  },
});
console.log(`subscribed to ${stop.subject} for control`);

const sub = nc.subscribe(`work.${subj}`, { callback: fn as MsgCallback<Msg> });
console.log(`subscribed to ${sub.subject} for processing requests`);

const ping = nc.subscribe(`ping.${subj}`, {
  callback: (_: Error | null, msg: Msg): void => {
    msg.respond();
  },
});
console.log(`subscribed to ${ping.subject} for liveness checks`);
