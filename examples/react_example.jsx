// MailBear: a React (hooks) contact form that submits via fetch and handles the
// JSON response. Includes a honeypot field.
//
// To add Cloudflare Turnstile (only needed if the server sets a Turnstile
// secret): load https://challenges.cloudflare.com/turnstile/v0/api.js, render a
// <div className="cf-turnstile" data-sitekey="..."> inside the form, and include
// the injected `cf-turnstile-response` field in the payload (e.g. read it from
// the form via FormData rather than component state).

import { useState } from "react";

const ENDPOINT = "https://mailbear.example.com/api/v1/form/YOUR_FORM_KEY";

const EMPTY = { name: "", email: "", subject: "", content: "", _gotcha: "" };

export default function ContactForm() {
  const [form, setForm] = useState(EMPTY);
  const [status, setStatus] = useState(null); // null | "sending" | "ok" | "error"
  const [message, setMessage] = useState("");

  const update = (event) =>
    setForm((prev) => ({ ...prev, [event.target.name]: event.target.value }));

  const submit = async (event) => {
    event.preventDefault();
    setStatus("sending");

    try {
      const response = await fetch(ENDPOINT, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });
      const body = await response.json();

      if (response.ok) {
        setStatus("ok");
        setMessage("Thanks — your message was sent.");
        setForm(EMPTY);
      } else {
        setStatus("error");
        setMessage(body.message || "Something went wrong.");
      }
    } catch {
      setStatus("error");
      setMessage("Network error — please try again.");
    }
  };

  return (
    <form onSubmit={submit}>
      <input name="name" placeholder="Name" value={form.name} onChange={update} />
      <input name="email" type="email" placeholder="Email" required value={form.email} onChange={update} />
      <input name="subject" placeholder="Subject" required value={form.subject} onChange={update} />
      <textarea name="content" placeholder="Message" required value={form.content} onChange={update} />

      {/* Honeypot: hidden from real users, left empty. */}
      <input
        name="_gotcha"
        tabIndex={-1}
        autoComplete="off"
        aria-hidden="true"
        value={form._gotcha}
        onChange={update}
        style={{ position: "absolute", left: "-9999px" }}
      />

      <button type="submit" disabled={status === "sending"}>
        {status === "sending" ? "Sending…" : "Send"}
      </button>

      {message && <p role="status">{message}</p>}
    </form>
  );
}
