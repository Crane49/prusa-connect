-- The orders the fleet's page of prints reads in, so each page walks an index
-- and stops at the limit instead of sorting every print the caller may see.
--
-- printer_job_by_start serves the page over every device and the page of
-- ended prints. printer_job_running holds only the prints with no end, a
-- handful at any time, so the page of running prints never walks the ended
-- ones. printer_job_by_device_start serves a page over one device, the id
-- breaking a tie on the start the way the page orders.
CREATE INDEX IF NOT EXISTS printer_job_by_start ON printer_job (started, printer_job_id);

CREATE INDEX IF NOT EXISTS printer_job_running ON printer_job (started, printer_job_id)
    WHERE ended IS NULL;

CREATE INDEX IF NOT EXISTS printer_job_by_device_start
    ON printer_job (device_id, started, printer_job_id);
