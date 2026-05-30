# PTR sync — incremental (delta) mode + 6:50 AM timing

The importer (sharepoint_import.py) supports two modes, auto-chosen by the
email subject it reads:
  * **PTR-EXPORT** -> incremental: UPSERT rows by SharePoint item ID (sp_item_id).
                     Merges the day's changes into the accumulated history.
  * **PTR-FULL**   -> full: wipe + reload (weekly resync; catches deletes) for
                     Blue Book / Payments / GPS. **Check Ins is upserted, not
                     wiped**, because that list exceeds the 5000-item page cap and
                     a full export only returns 5000 rows — wiping would lose the
                     accumulated history. (Handled automatically in the importer.)
It also self-adds columns it needs (sp_item_id + a unique index, and the GPS
switched_to / switched_gps_date / notes columns).

--------------------------------------------------------------------------
## DONE (built in-browser on make.gov.powerautomate.us)

[x] PTR Daily Export — Recurrence = 6:50 AM Eastern, daily.
[x] PTR Daily Export — Filter Query  Modified ge '@{addHours(utcNow(),-25)}'
    added to all 4 Get items (25h overlap window; upsert de-dupes).
[x] PTR Weekly Full — Save-As copy: filters removed (full pull), email Subject
    = PTR-FULL, Recurrence = Weekly / Sunday / 6:50 AM Eastern. Turned ON.

## STILL TO DO — you

1) **Index "Modified" on the Check Ins list** (REQUIRED — it has >5000 items;
   without the index the daily Check Ins filter throws a threshold error):
     SharePoint -> Check Ins list -> gear -> List settings -> Indexed columns
       -> Create a new index -> Primary column = Modified -> Create.
   (Blue Book / Payments / GPS are under 5000, so they don't need this.)

2) **Deploy the updated importer to ptr1** (section A below).
3) **Move the import timer to 7:10 ET** (section B below).
--------------------------------------------------------------------------

## A. Deploy the updated code to ptr1

On rzr (Windows, in PowerShell):
    scp "C:\Users\alexa\OneDrive\Documents\sharepoint_import.py" "C:\Users\alexa\OneDrive\Documents\queries_ext.py" alex@100.85.252.79:~/

On ptr1:
    sudo install -m 0644 ~/queries_ext.py       /opt/ptr-knoxc/webapp/queries_ext.py
    sudo install -m 0755 ~/sharepoint_import.py /opt/ptr-knoxc/webapp/sharepoint_import.py
    sudo chown ptrapp:ptrapp /opt/ptr-knoxc/webapp/queries_ext.py /opt/ptr-knoxc/webapp/sharepoint_import.py

    # one-time CLEAN reseed: adds sp_item_id + GPS columns, loads with keys.
    # (reads the latest PTR-FULL email if present, else the PTR-EXPORT.)
    sudo bash -c 'set -a; . /etc/ptr-import.env; set +a; python3 /opt/ptr-knoxc/webapp/sharepoint_import.py --full'
    sudo chown ptrapp:ptrapp /opt/ptr-knoxc/db/kh222.db*
    sudo systemctl restart ptr-webapp
    curl -s http://127.0.0.1:8000/api/refresh ; echo

## B. Move the import to run after the 6:50 email

    sudo sed -i 's|^OnCalendar=.*|OnCalendar=*-*-* 07:10:00 America/New_York|' /etc/systemd/system/ptr-import.timer
    sudo systemctl daemon-reload && sudo systemctl restart ptr-import.timer
    systemctl list-timers ptr-import.timer --no-pager     # next run should read 07:10 Eastern

## How it runs day-to-day

  06:50 ET  Power Automate emails the CSVs (PTR-EXPORT Mon-Sat, PTR-FULL Sun).
  07:10 ET  ptr1's ptr-import.timer fires sharepoint_import.py, which reads the
            newest matching email over IMAP, upserts (or Sunday-reloads) the
            raw_* tables, and refreshes the app cache.
