---
title: Logs
---

# Logs

The **Logs** tab shows the last lines of the website’s logs, newest at the bottom. It is read-only: nothing can be edited or deleted here.

## Log types

Pick one at the top right:

* **Access**: every request sent to the website.
* **Error**: Nginx errors.
* **Application**: output from a Binary or mise website’s systemd service.

## Search and line count

* **Search** filters the lines containing the text you enter (up to 100 characters).
* **Lines** sets how many of the last lines to show.
* Click **Show** to apply.

Above the log box you see the file path, its size, the number of matching lines, and a **Truncated** badge when the result is too long. If the file does not exist yet, the panel shows the path it looked for.

## Log rotation

Old logs are rotated daily and 14 copies are kept.

Tip

When an application website returns a 500 or 502 error, open the **Error** log first, then **Application** to find the cause.
