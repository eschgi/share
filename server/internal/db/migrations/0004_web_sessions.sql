-- 0004: browsers sign in too, as devices of the client 'web', with the key in a cookie. A
-- browser that signed in at home (plain http, or the https port from the home network) is
-- home-only: its cookie would also reach any device with that address on other networks, so
-- the session works only at home.
ALTER TABLE devices ADD COLUMN client TEXT NOT NULL DEFAULT 'app' CHECK (client IN ('app', 'web'));
ALTER TABLE devices ADD COLUMN home_only INTEGER NOT NULL DEFAULT 0 CHECK (home_only IN (0, 1));
