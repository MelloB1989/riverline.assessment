import { auth } from "@clerk/nextjs/server";

import ChatShell from "./chat-shell";

export default async function ChatPage() {
  await auth.protect();

  return <ChatShell />;
}
