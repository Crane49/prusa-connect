-- How a print ended, in the driver's word, written with the end and frozen with
-- the rest of the row: finished, failed, cancelled or unknown. It is empty while
-- the print runs, and for a row whose end was written without one.
ALTER TABLE printer_job ADD COLUMN outcome TEXT NOT NULL DEFAULT '';
