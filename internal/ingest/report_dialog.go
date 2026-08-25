package ingest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"urgentry/internal/auth"
	"urgentry/internal/httputil"
	"urgentry/internal/sqlite"
	"urgentry/pkg/dsn"
)

const (
	reportDialogMaxName     = 128
	reportDialogMaxEmail    = 75
	reportDialogMaxComments = 4096
)

type reportDialogConfig struct {
	Endpoint       string `json:"endpoint"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	Subtitle2      string `json:"subtitle2"`
	LabelName      string `json:"labelName"`
	LabelEmail     string `json:"labelEmail"`
	LabelComments  string `json:"labelComments"`
	LabelClose     string `json:"labelClose"`
	LabelSubmit    string `json:"labelSubmit"`
	ErrorGeneric   string `json:"errorGeneric"`
	ErrorFormEntry string `json:"errorFormEntry"`
	SuccessMessage string `json:"successMessage"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	ColorScheme    string `json:"colorScheme,omitempty"`
}

// ReportDialogHandler implements Sentry's legacy crash-report dialog endpoint.
// The browser SDK loads this endpoint as JavaScript, and the returned script
// renders a modal that POSTs a user report back to the same URL.
func ReportDialogHandler(keys auth.KeyStore, feedback *sqlite.FeedbackStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setReportDialogCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST, OPTIONS")
			httputil.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		projectKey, eventID, ok := resolveReportDialogRequest(w, r, keys)
		if !ok {
			return
		}

		if r.Method == http.MethodPost {
			handleReportDialogSubmission(w, r, feedback, projectKey.ProjectID, eventID)
			return
		}

		config := reportDialogConfigFromRequest(r)
		payload, err := json.Marshal(config)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "Failed to render report dialog")
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, reportDialogScript, payload)
	})
}

func setReportDialogCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "content-type, authorization, x-requested-with, baggage, sentry-trace")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
}

func resolveReportDialogRequest(w http.ResponseWriter, r *http.Request, keys auth.KeyStore) (*auth.ProjectKey, string, bool) {
	parsed, err := dsn.Parse(r.URL.Query().Get("dsn"))
	if err != nil || keys == nil {
		writeReportDialogError(w, http.StatusNotFound, "dsn", "Missing or invalid parameter.")
		return nil, "", false
	}

	projectKey, err := keys.LookupKey(r.Context(), parsed.PublicKey)
	if err != nil || projectKey.Status != "active" || !dsn.ProjectIDMatches(projectKey.ProjectID, parsed.ProjectID) {
		writeReportDialogError(w, http.StatusNotFound, "dsn", "Missing or invalid parameter.")
		return nil, "", false
	}

	eventID, valid := normalizeReportDialogEventID(r.URL.Query().Get("eventId"))
	if !valid {
		writeReportDialogError(w, http.StatusBadRequest, "eventId", "Missing or invalid parameter.")
		return nil, "", false
	}
	return projectKey, eventID, true
}

func normalizeReportDialogEventID(raw string) (string, bool) {
	eventID := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", ""))
	if len(eventID) != 32 {
		return "", false
	}
	for _, char := range eventID {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return "", false
		}
	}
	return eventID, true
}

func writeReportDialogError(w http.ResponseWriter, status int, field, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{field: message})
}

func handleReportDialogSubmission(w http.ResponseWriter, r *http.Request, feedback *sqlite.FeedbackStore, projectID, eventID string) {
	if feedback == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "Feedback storage is unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "Invalid form data")
		return
	}

	name := strings.TrimSpace(r.Form.Get("name"))
	email := strings.TrimSpace(r.Form.Get("email"))
	comments := strings.TrimSpace(r.Form.Get("comments"))
	errors := validateReportDialogForm(name, email, comments)
	if len(errors) != 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": errors})
		return
	}

	if err := feedback.SaveFeedback(r.Context(), projectID, eventID, name, email, comments); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "Failed to save feedback")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{})
}

func validateReportDialogForm(name, email, comments string) map[string][]string {
	errors := make(map[string][]string)
	if name == "" || utf8.RuneCountInString(name) > reportDialogMaxName {
		errors["name"] = []string{"Enter a name of at most 128 characters."}
	}
	if email == "" || utf8.RuneCountInString(email) > reportDialogMaxEmail || !validReportDialogEmail(email) {
		errors["email"] = []string{"Enter a valid email address of at most 75 characters."}
	}
	if comments == "" || utf8.RuneCountInString(comments) > reportDialogMaxComments {
		errors["comments"] = []string{"Enter feedback of at most 4096 characters."}
	}
	return errors
}

func validReportDialogEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(address.Address, "@")
}

func reportDialogConfigFromRequest(r *http.Request) reportDialogConfig {
	query := r.URL.Query()
	return reportDialogConfig{
		Endpoint:       reportDialogEndpoint(r),
		Title:          reportDialogOption(query, "title", "It looks like we're having issues."),
		Subtitle:       reportDialogOption(query, "subtitle", "Our team has been notified."),
		Subtitle2:      reportDialogOption(query, "subtitle2", "If you'd like to help, tell us what happened below."),
		LabelName:      reportDialogOption(query, "labelName", "Name"),
		LabelEmail:     reportDialogOption(query, "labelEmail", "Email"),
		LabelComments:  reportDialogOption(query, "labelComments", "What happened?"),
		LabelClose:     reportDialogOption(query, "labelClose", "Close"),
		LabelSubmit:    reportDialogOption(query, "labelSubmit", "Submit Crash Report"),
		ErrorGeneric:   reportDialogOption(query, "errorGeneric", "An unknown error occurred while submitting your report. Please try again."),
		ErrorFormEntry: reportDialogOption(query, "errorFormEntry", "Some fields were invalid. Please correct the errors and try again."),
		SuccessMessage: reportDialogOption(query, "successMessage", "Your feedback has been sent. Thank you!"),
		Name:           query.Get("name"),
		Email:          query.Get("email"),
		ColorScheme:    strings.ToLower(strings.TrimSpace(query.Get("colorScheme"))),
	}
}

func reportDialogOption(query url.Values, key, fallback string) string {
	if value, ok := query[key]; ok && len(value) > 0 {
		return value[0]
	}
	return fallback
}

func reportDialogEndpoint(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host + r.URL.RequestURI()
}

const reportDialogScript = `(function(window, document, JSON) {
  'use strict';
  var config = %s;
  var existing = document.getElementById('urgentry-sentry-report-dialog');
  if (existing && existing.parentNode) existing.parentNode.removeChild(existing);

  var scheme = config.colorScheme;
  try {
    if (!scheme && window.Sentry && typeof window.Sentry.getClient === 'function') {
      var client = window.Sentry.getClient();
      var options = client && typeof client.getOptions === 'function' ? client.getOptions() : null;
      scheme = options && options.colorScheme;
    }
  } catch (_) {}
  if (scheme !== 'dark' && scheme !== 'light') {
    scheme = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  var wrapper = document.createElement('div');
  wrapper.id = 'urgentry-sentry-report-dialog';
  wrapper.className = 'urgentry-report-dialog-wrapper urgentry-report-dialog-' + scheme;
  wrapper.setAttribute('role', 'presentation');

  var style = document.createElement('style');
  style.textContent = [
    '.urgentry-report-dialog-wrapper{position:fixed;z-index:2147483647;inset:0;display:flex;align-items:flex-start;justify-content:center;padding:4vh 16px;box-sizing:border-box;background:rgba(3,7,18,.72);overflow:auto;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}',
    '.urgentry-report-dialog{width:min(620px,100%%);box-sizing:border-box;padding:30px;border:1px solid #d8dee9;border-radius:12px;background:#fff;color:#374151;box-shadow:0 24px 80px rgba(0,0,0,.38)}',
    '.urgentry-report-dialog-dark .urgentry-report-dialog{border-color:#303746;background:#161b22;color:#c9d1d9}',
    '.urgentry-report-dialog h2{margin:0 0 8px;color:#111827;font-size:25px;line-height:1.25;text-align:center}',
    '.urgentry-report-dialog-dark h2{color:#f0f3f6}',
    '.urgentry-report-dialog-subtitle{margin:0 0 24px;color:#667085;font-size:14px;line-height:1.5;text-align:center;white-space:pre-wrap}',
    '.urgentry-report-dialog-dark .urgentry-report-dialog-subtitle{color:#9da7b3}',
    '.urgentry-report-dialog-field{display:block;margin:0 0 17px}',
    '.urgentry-report-dialog-field span{display:block;margin:0 0 6px;font-size:12px;font-weight:650;text-transform:uppercase;letter-spacing:.04em}',
    '.urgentry-report-dialog input,.urgentry-report-dialog textarea{width:100%%;box-sizing:border-box;padding:10px 12px;border:1px solid #c9d1dc;border-radius:7px;background:#fff;color:#111827;font:inherit;outline:none}',
    '.urgentry-report-dialog-dark input,.urgentry-report-dialog-dark textarea{border-color:#3b4351;background:#0d1117;color:#e6edf3}',
    '.urgentry-report-dialog textarea{min-height:120px;resize:vertical}',
    '.urgentry-report-dialog input:focus,.urgentry-report-dialog textarea:focus{border-color:#5b7cfa;box-shadow:0 0 0 3px rgba(91,124,250,.18)}',
    '.urgentry-report-dialog-invalid{border-color:#e5484d!important}',
    '.urgentry-report-dialog-message{min-height:20px;margin:0 0 14px;font-size:14px;text-align:center}',
    '.urgentry-report-dialog-error{color:#d92d20}',
    '.urgentry-report-dialog-success{color:#16803c}',
    '.urgentry-report-dialog-actions{display:flex;align-items:center;gap:12px;margin-top:6px}',
    '.urgentry-report-dialog button{border:0;border-radius:7px;padding:10px 16px;font:inherit;font-weight:650;cursor:pointer}',
    '.urgentry-report-dialog-submit{background:#5b7cfa;color:#fff}',
    '.urgentry-report-dialog-submit:hover{background:#4969e8}',
    '.urgentry-report-dialog-submit:disabled{cursor:wait;opacity:.65}',
    '.urgentry-report-dialog-close{background:transparent;color:inherit}',
    '@media(max-width:560px){.urgentry-report-dialog-wrapper{padding:0}.urgentry-report-dialog{min-height:100%%;border-radius:0;padding:22px 18px}}'
  ].join('');

  var dialog = document.createElement('section');
  dialog.className = 'urgentry-report-dialog';
  dialog.setAttribute('role', 'dialog');
  dialog.setAttribute('aria-modal', 'true');
  dialog.setAttribute('aria-labelledby', 'urgentry-report-dialog-title');

  function element(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }
  var title = element('h2', '', config.title);
  title.id = 'urgentry-report-dialog-title';
  var subtitle = element('p', 'urgentry-report-dialog-subtitle', config.subtitle + (config.subtitle2 ? ' ' + config.subtitle2 : ''));
  var form = document.createElement('form');
  var message = element('div', 'urgentry-report-dialog-message', '');

  function field(name, label, type, value, maxLength) {
    var container = element('label', 'urgentry-report-dialog-field');
    container.appendChild(element('span', '', label));
    var input = document.createElement(type === 'textarea' ? 'textarea' : 'input');
    input.name = name;
    if (type !== 'textarea') input.type = type;
    input.value = value || '';
    input.maxLength = maxLength;
    input.required = true;
    container.appendChild(input);
    form.appendChild(container);
    return input;
  }

  var nameInput = field('name', config.labelName, 'text', config.name, 128);
  var emailInput = field('email', config.labelEmail, 'email', config.email, 75);
  var commentsInput = field('comments', config.labelComments, 'textarea', '', 4096);
  var actions = element('div', 'urgentry-report-dialog-actions');
  var submit = element('button', 'urgentry-report-dialog-submit', config.labelSubmit);
  submit.type = 'submit';
  var closeButton = element('button', 'urgentry-report-dialog-close', config.labelClose);
  closeButton.type = 'button';
  actions.appendChild(submit);
  actions.appendChild(closeButton);
  form.appendChild(message);
  form.appendChild(actions);
  dialog.appendChild(title);
  dialog.appendChild(subtitle);
  dialog.appendChild(form);
  wrapper.appendChild(style);
  wrapper.appendChild(dialog);

  function close() {
    document.removeEventListener('keydown', onKeyDown);
    if (wrapper.parentNode) wrapper.parentNode.removeChild(wrapper);
    if (window.parent) window.parent.postMessage('__sentry_reportdialog_closed__', '*');
  }
  function onKeyDown(event) {
    if (event.key === 'Escape' || event.keyCode === 27) close();
  }
  closeButton.onclick = close;
  wrapper.onclick = function(event) { if (event.target === wrapper) close(); };
  document.addEventListener('keydown', onKeyDown);

  form.onsubmit = function(event) {
    event.preventDefault();
    submit.disabled = true;
    message.textContent = '';
    message.className = 'urgentry-report-dialog-message';
    nameInput.classList.remove('urgentry-report-dialog-invalid');
    emailInput.classList.remove('urgentry-report-dialog-invalid');
    commentsInput.classList.remove('urgentry-report-dialog-invalid');

    var body = 'name=' + encodeURIComponent(nameInput.value) + '&email=' + encodeURIComponent(emailInput.value) + '&comments=' + encodeURIComponent(commentsInput.value);
    var xhr = new XMLHttpRequest();
    xhr.onreadystatechange = function() {
      if (xhr.readyState !== 4) return;
      submit.disabled = false;
      if (xhr.status === 200) {
        message.textContent = config.successMessage;
        message.className += ' urgentry-report-dialog-success';
        nameInput.parentNode.style.display = 'none';
        emailInput.parentNode.style.display = 'none';
        commentsInput.parentNode.style.display = 'none';
        submit.style.display = 'none';
      } else if (xhr.status === 400) {
        try {
          var response = JSON.parse(xhr.responseText);
          var errors = response.errors || {};
          if (errors.name) nameInput.classList.add('urgentry-report-dialog-invalid');
          if (errors.email) emailInput.classList.add('urgentry-report-dialog-invalid');
          if (errors.comments) commentsInput.classList.add('urgentry-report-dialog-invalid');
          message.textContent = config.errorFormEntry;
        } catch (_) {
          message.textContent = config.errorGeneric;
        }
        message.className += ' urgentry-report-dialog-error';
      } else {
        message.textContent = config.errorGeneric;
        message.className += ' urgentry-report-dialog-error';
      }
    };
    xhr.open('POST', config.endpoint, true);
    xhr.setRequestHeader('Content-Type', 'application/x-www-form-urlencoded');
    xhr.send(body);
  };

  function attach() {
    document.body.appendChild(wrapper);
    window.setTimeout(function() { commentsInput.focus(); }, 0);
    if (window.sentryEmbedCallback && typeof window.sentryEmbedCallback === 'function') {
      window.sentryEmbedCallback({element: wrapper, close: close});
    }
  }
  if (document.body) {
    attach();
  } else {
    document.addEventListener('DOMContentLoaded', attach, {once: true});
  }
})(window, document, JSON);`
