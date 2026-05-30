# Daily SharePoint -> PTR Lookup refresh (no-premium route)

Keeps the self-hosted lookup's data current automatically, with no Microsoft
admin rights and no premium Power Automate license.

```
SharePoint lists -> Power Automate (daily, standard connectors)
                 -> emails 4 CSVs (subject "PTR-EXPORT")
                 -> [mailbox]  -> ptr1 reads it over IMAP (ptr-import.timer)
                 -> reloads raw_* tables in /opt/ptr-knoxc/db/kh222.db
                 -> lookup serves fresh data within ~60s
```

> NOTE: this route emails defendant data to the mailbox you choose. Use a
> dedicated mailbox, restrict who can read it, and treat it as sensitive.

---

## 1. Mailbox (one-time)

Use a dedicated mailbox the server can read over IMAP. Gmail is simplest:
1. Create/choose a Gmail just for this.
2. Turn on 2-Step Verification, then create an **App Password** (Google Account ->
   Security -> App passwords). Copy the 16-char password (use it without spaces).

## 2. Power Automate flow (one-time, in your M365 account)

make.powerautomate.com -> **Create -> Scheduled cloud flow**.

1. **Trigger -- Recurrence:** every 1 day at ~05:00.
2. For **each** of the four lists (Blue Book, Check Ins, Payments, GPS 48 Hours):
   - **SharePoint -> Get items.** Site Address = your Pre-Trial site; List Name =
     the list. Then **... -> Settings -> Pagination = On, Threshold = 100000**.
   - **Data Operation -> Create CSV table.** From = the **value** output of that
     Get items; Columns = **Automatic**.
3. **Office 365 Outlook -> Send an email (V2):**
   - **To:** your dedicated mailbox.
   - **Subject:** `PTR-EXPORT`  (exact -- the server filters on it).
   - **Attachments:** add 4 -- names `bluebook.csv`, `checkins.csv`,
     `payments.csv`, `gps.csv`; each Content = the matching **Create CSV table**.
4. **Save**, then **Test -> Manually -> Run** once so a first email lands.

All four actions are standard connectors -- no Premium tag blocks them.

## 3. ptr1 receiver (one-time)

Copy the updated webapp (which includes `sharepoint_import.py`) to ptr1 and
install the timer. From your workstation:

```powershell
scp "C:\Users\alexa\Projects\pretrial-knoxc\webapp\sharepoint_import.py" alex@ptr1:~/
```

On ptr1:

```bash
sudo install -m 0755 ~/sharepoint_import.py /opt/ptr-knoxc/webapp/sharepoint_import.py
sudo chown ptrapp:ptrapp /opt/ptr-knoxc/webapp/sharepoint_import.py
# (or, if you re-copied the whole package: sudo bash deploy/setup_import.sh)
```

Install the env + timer (if you didn't run setup_import.sh):

```bash
sudo cp deploy/ptr-import.env.example /etc/ptr-import.env
sudo nano /etc/ptr-import.env          # fill IMAP_USER + IMAP_PASS (+ IMAP_FROM optional)
sudo chmod 600 /etc/ptr-import.env
sudo cp deploy/ptr-import.service deploy/ptr-import.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ptr-import.timer
```

## 4. Test the whole pipe

After the flow has sent one `PTR-EXPORT` email:

```bash
sudo systemctl start ptr-import.service
journalctl -u ptr-import.service -n 40 --no-pager
```

You want lines like `bluebook: loaded N rows into raw_blue_book ... commit OK`.
Then check the lookup reflects it: `curl -s http://127.0.0.1:8000/api/refresh`
(clears the 60s cache) and reload the site.

## How it works / notes

- `sharepoint_import.py` matches CSV headers to DB columns by fuzzy name, so it
  tolerates SharePoint's column-name quirks. It REPLACES each table's rows in one
  transaction (WAL + busy_timeout) so the running lookup never sees a half-load.
- Only the four lookup tables are refreshed (`raw_blue_book`, `raw_check_ins`,
  `raw_payments`, `raw_gps_48_hours`). The master list isn't needed for the lookup.
- A missing/empty attachment leaves that table unchanged (won't wipe data).
- `--dir /path` mode imports CSVs straight from a folder instead of IMAP -- handy
  for a manual drop or re-test.
- First real run: confirm every column you care about mapped (the log lists the
  matched columns per table). If one didn't match, tell Claude the exact CSV
  header and it'll be added to the alias list.

## Timer

```bash
systemctl list-timers ptr-import.timer      # next run
journalctl -u ptr-import.service -f         # live log
```
