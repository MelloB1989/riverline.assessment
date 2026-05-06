import {
  pgTable,
  varchar,
  timestamp,
  integer,
  text,
  json,
  boolean,
  index,
  real,
  uniqueIndex,
} from "drizzle-orm/pg-core";

export const users = pgTable("users", {
  id: varchar("id").primaryKey().notNull(),
  first_name: varchar("first_name").notNull(),
  last_name: varchar("last_name").notNull(),
  email: varchar("email").notNull(),
  phone: varchar("phone"),
  dob: timestamp("dob").notNull(),
  gender: varchar("gender").notNull(),
  pfp: varchar("pfp").default(""),
  bio: text("bio").notNull(),
  extra: json("extra").default({}),
  created_at: timestamp("created_at").defaultNow().notNull(),
  updated_at: timestamp("updated_at").defaultNow().notNull(),
  billing_address: json("billing_address").default({
    country: "IN",
    state: "",
    city: "",
    street: "",
    zipcode: "",
  }),
});
