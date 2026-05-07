ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "tool_calls";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "tool_call_id";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "images";--> statement-breakpoint
ALTER TABLE "agent_messages" DROP COLUMN IF EXISTS "files";
