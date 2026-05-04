import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { JsonForms } from "@jsonforms/react";
import { vanillaCells, vanillaRenderers } from "@jsonforms/vanilla-renderers";
import {
  CollapsibleGroupRenderer,
  collapsibleGroupTester,
} from "./renderers/CollapsibleGroupRenderer";
import {
  LongLabelRenderer,
  longLabelTester,
} from "./renderers/LongLabelRenderer";
import {
  LongTextDisplayRenderer,
  longTextDisplayTester,
} from "./renderers/LongTextDisplayRenderer";

const renderers = [
  ...vanillaRenderers,
  { tester: collapsibleGroupTester, renderer: CollapsibleGroupRenderer },
  { tester: longLabelTester, renderer: LongLabelRenderer },
  { tester: longTextDisplayTester, renderer: LongTextDisplayRenderer },
];

type MountOptions = {
  specUrl: string;
  appId?: string;
  errId?: string;
  submitBtnId?: string;
  payloadInputId?: string;
  formId?: string;
};

function getEl<T extends HTMLElement>(id: string | undefined): T | null {
  if (!id) return null;
  const el = document.getElementById(id);
  return el as T | null;
}

export function mount(opts: MountOptions) {
  const appId = opts.appId ?? "app";
  const errId = opts.errId ?? "err";
  const submitBtnId = opts.submitBtnId ?? "submitBtn";
  const payloadInputId = opts.payloadInputId ?? "payload_json";
  const formId = opts.formId ?? "submitForm";

  const elApp = getEl<HTMLDivElement>(appId);
  const elErr = getEl<HTMLDivElement>(errId);
  const elSubmitBtn = getEl<HTMLButtonElement>(submitBtnId);
  const elPayload = getEl<HTMLInputElement>(payloadInputId);
  const form = getEl<HTMLFormElement>(formId);

  function showError(message: string) {
    if (!elErr) return;
    elErr.className = "err";
    elErr.textContent = message;
    elErr.style.display = "block";
  }

  function App() {
    const [schema, setSchema] = useState<any>(null);
    const [uischema, setUiSchema] = useState<any>(undefined);
    const [data, setData] = useState<any>({});
    const [errors, setErrors] = useState<any[]>([]);
    const [submitLabel, setSubmitLabel] = useState<string>("Submit");

    useEffect(() => {
      (async () => {
        try {
          const res = await fetch(opts.specUrl, { headers: { Accept: "application/json" } });
          if (!res.ok) {
            showError("Failed to load form spec.");
            return;
          }
          const spec = await res.json();
          if (!spec || typeof spec !== "object" || !spec.schema) {
            showError("Invalid form spec.");
            return;
          }
          setSchema(spec.schema);
          if (spec.uischema) setUiSchema(spec.uischema);
          if (spec.data !== undefined && spec.data !== null) setData(spec.data);
          if (typeof spec.submit_label === "string" && spec.submit_label.trim()) {
            setSubmitLabel(spec.submit_label.trim());
          }
          if (typeof spec.renderer === "string" && spec.renderer.trim() && spec.renderer.trim() !== "vanilla") {
            showError("Unsupported form renderer.");
          }
        } catch {
          showError("Failed to load form spec.");
        }
      })();
    }, []);

    useEffect(() => {
      if (elSubmitBtn) elSubmitBtn.textContent = submitLabel;
    }, [submitLabel]);

    const hasErrors = useMemo(() => Array.isArray(errors) && errors.length > 0, [errors]);
    useEffect(() => {
      if (elSubmitBtn) elSubmitBtn.disabled = hasErrors;
    }, [hasErrors]);

    useEffect(() => {
      if (!elPayload) return;
      try {
        elPayload.value = JSON.stringify(data ?? {});
      } catch {
        elPayload.value = "{}";
      }
    }, [data]);

    if (!schema) {
      return React.createElement("div", null, "Loading...");
    }

    return React.createElement(JsonForms, {
      schema,
      uischema,
      data,
      renderers,
      cells: vanillaCells,
      onChange: ({ data, errors }: any) => {
        if (data !== undefined) setData(data);
        if (Array.isArray(errors)) setErrors(errors);
      }
    });
  }

  if (form) {
    form.addEventListener("submit", () => {
      try {
        if (elPayload && !elPayload.value) elPayload.value = "{}";
      } catch {}
    });
  }

  if (!elApp) return;
  createRoot(elApp).render(React.createElement(App));
}

