ALTER TABLE "agent_messages" ADD COLUMN "tool_calls" json DEFAULT '[]'::json;--> statement-breakpoint
ALTER TABLE "agent_messages" ADD COLUMN "tool_call_id" varchar;--> statement-breakpoint
ALTER TABLE "agent_messages" ADD COLUMN "images" json DEFAULT '[]'::json;--> statement-breakpoint
ALTER TABLE "agent_messages" ADD COLUMN "files" json DEFAULT '[]'::json;