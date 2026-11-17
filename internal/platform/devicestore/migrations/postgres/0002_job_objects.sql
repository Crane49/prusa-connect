-- The objects of a print that can be cancelled one by one, as the last reading
-- of the print described them. The value is a JSON document the host writes:
-- how many objects the machine reported, and the objects themselves, each with
-- its id, its name when the machine gave one, and whether it was cancelled. It
-- is empty for a print whose machine said nothing about its objects.
ALTER TABLE printer_job ADD COLUMN objects TEXT NOT NULL DEFAULT '';
