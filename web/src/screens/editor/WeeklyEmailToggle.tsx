import { useEffect, useState } from "react";
import { api, errorMessage } from "../../api";
import { useUser } from "../../auth";
import { Switch } from "../../components/ui";

/** Opt-in switch for the weekly stats email. The setting is per account. */
export function WeeklyEmailToggle() {
  const user = useUser();
  const [on, setOn] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .accountSettings()
      .then((s) => !cancelled && setOn(s.weekly_stats_email))
      .catch((err: unknown) => !cancelled && setError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, []);

  async function change(next: boolean) {
    setSaving(true);
    setError(null);
    try {
      const s = await api.updateAccountSettings({ weekly_stats_email: next });
      setOn(s.weekly_stats_email);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  const unverified = !user.email_verified && !on;

  return (
    <section className="card section" aria-labelledby="weekly-email-heading">
      <div className="section-row">
        <div className="section-intro">
          <h2 id="weekly-email-heading">Weekly email</h2>
          <p className="muted small">
            Every Monday, a summary of last week's views, clicks, referrers and countries for all your pages, sent to{" "}
            {user.email}.
          </p>
        </div>
        <Switch
          checked={on === true}
          onChange={(v) => void change(v)}
          label="Send me weekly stats"
          disabled={on === null || saving || unverified}
        />
      </div>
      {unverified && <p className="muted small">Verify your email address to turn this on.</p>}
      {error && (
        <p className="field-error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
