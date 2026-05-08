DO $$ BEGIN
 CREATE TYPE "public"."offer_status" AS ENUM('proposed', 'accepted', 'rejected');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
ALTER TABLE "resolution_offers" ADD COLUMN "status" "offer_status" DEFAULT 'proposed' NOT NULL;