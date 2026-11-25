---
title: Cron
---

# Cron

The **Cron** tab manages a website’s scheduled jobs. Each job runs as the website’s own system account, so it cannot touch another website’s files.

Cron jobs are scheduled commands that finish after each run. For a continuously running queue worker or other supervised process, use [Background services](background-services.md) instead.

## Job list

The table lists each job with four columns:

* **Runs**: the schedule in readable form, with the raw cron expression when it differs.
* **Command**: the command that will run.
* **State**: On or Off.
* **Actions**: **Pause** / **Resume** and **Delete**.

Pausing keeps the job but stops it from running, which is handy for a temporary shut-off.

## Adding a job

1. Select the **Add scheduled job** plus icon in the page header.
2. In **Run**, pick a preset schedule (the default is 3 AM every day).
3. For a custom schedule, fill in **Or a custom schedule** with five fields separated by spaces: `minute hour day month weekday`, for example `*/10 8-18 * * 1-5`. When this field has content, it replaces the preset on the left.
4. Enter the **Command**, then click **Add**.

Each website has a maximum number of jobs, shown in the add dialog.

## Writing the command

* The command runs through `/bin/sh`, with the website’s home directory as the working directory.
* Use full paths, for example `php8.4 /home/<website-name>/htdocs/cron.php`.
* Output is discarded. To keep it, append `>> ~/cron.log 2>&1` to the command.

Tip

For WordPress, disable the built-in `WP_CRON` and call `wp-cron.php` from here on a fixed schedule, so scheduled tasks do not depend on visitor traffic.
