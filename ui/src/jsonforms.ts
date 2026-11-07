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

type E2EEApi = {
  deriveKey: (password: string, salt: string, iterations: number) => Promise<CryptoKey>;
  decryptText: (token: string, key: CryptoKey) => Promise<string>;
  decryptJSONValue: (value: unknown, key: CryptoKey) => Promise<unknown>;
  decryptStringsDeep: <T = unknown>(value: T, key: CryptoKey) => Promise<T>;
  encryptJSON: (value: unknown, key: CryptoKey) => Promise<string>;
};

declare global {
  interface Window {
    Ask4MeE2EE?: E2EEApi;
  }
}

type MountOptions = {
  specUrl: string;
  appId?: string;
  errId?: string;
  submitBtnId?: string;
  payloadInputId?: string;
  formId?: string;
  cryptoKey?: CryptoKey;
  e2eeAnswers?: boolean;
  onReady?: () => void;
  onError?: (message: string) => void;
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
    if (elErr) {
      elErr.className = "err";
      elErr.textContent = message;
      elErr.style.display = "block";
    }
    opts.onError?.(message);
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

          let schema = spec.schema;
          let uischema = spec.uischema;
          let data = spec.data;
          let submitLabelValue = spec.submit_label;

          if (opts.cryptoKey) {
            const api = window.Ask4MeE2EE;
            if (!api) {
              showError("Failed to load decryptor.");
              return;
            }
            schema = await api.decryptStringsDeep(schema, opts.cryptoKey);
            if (uischema) uischema = await api.decryptStringsDeep(uischema, opts.cryptoKey);
            if (data !== undefined && data !== null) {
              data = await api.decryptJSONValue(data, opts.cryptoKey);
            }
            if (typeof submitLabelValue === "string" && submitLabelValue.trim()) {
              submitLabelValue = await api.decryptStringsDeep(submitLabelValue, opts.cryptoKey);
            }
          }

          setSchema(schema);
          if (uischema) setUiSchema(uischema);
          if (data !== undefined && data !== null) setData(data);
          if (typeof submitLabelValue === "string" && submitLabelValue.trim()) {
            setSubmitLabel(submitLabelValue.trim());
          }
          if (typeof spec.renderer === "string" && spec.renderer.trim() && spec.renderer.trim() !== "vanilla") {
            showError("Unsupported form renderer.");
          }
          setTimeout(() => opts.onReady?.(), 0);
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

    useEffect(() => {
      if (!form) return;
      const onSubmit = (e: Event) => {
        if (!opts.e2eeAnswers || !opts.cryptoKey) {
          try {
            if (elPayload && !elPayload.value) elPayload.value = "{}";
          } catch {}
          return;
        }
        e.preventDefault();
        const api = window.Ask4MeE2EE;
        if (!api) {
          showError("Failed to load decryptor.");
          return;
        }
        api.encryptJSON(data ?? {}, opts.cryptoKey)
          .then((token) => {
            if (elPayload) elPayload.value = JSON.stringify(token);
            form.submit();
          })
          .catch(() => showError("加密失败，请重试。"));
      };
      form.addEventListener("submit", onSubmit);
      return () => form.removeEventListener("submit", onSubmit);
    }, [form, data, elPayload, opts.cryptoKey, opts.e2eeAnswers]);

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

  if (!elApp) return;
  createRoot(elApp).render(React.createElement(App));
}

