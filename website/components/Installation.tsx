import React, { useState } from 'react';
import { Copy, Check } from 'lucide-react';
import { useLanguage } from '../LanguageContext';

export const Installation: React.FC = () => {
  const [copied, setCopied] = useState<string | null>(null);
  const [method, setMethod] = useState<'npm' | 'docker'>('npm');
  const { t } = useLanguage();

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopied(id);
    setTimeout(() => setCopied(null), 2000);
  };

  const CodeBlock = ({ id, label, command }: { id: string, label: string, command: string }) => (
    <div className="mb-6">
      <div className="flex justify-between items-center mb-2">
        <span className="text-sm font-medium text-slate-400">{label}</span>
      </div>
      <div className="relative group">
        <div className="bg-slate-900 border border-slate-800 rounded-lg p-4 font-mono text-sm text-emerald-400 overflow-x-auto custom-scrollbar whitespace-pre">
          {command}
        </div>
        <button
          onClick={() => copyToClipboard(command, id)}
          className="absolute right-3 top-3 p-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-md opacity-0 group-hover:opacity-100 transition-all border border-slate-700"
          aria-label="Copy command"
        >
          {copied === id ? <Check size={16} className="text-emerald-500" /> : <Copy size={16} />}
        </button>
      </div>
    </div>
  );

  const dockerRunCmd = `docker run -d \\
  --name ask4me \\
  -p 8080:8080 \\
  -v $(pwd)/ask4me.db:/data/ask4me.db \\
  -e ASK4ME_BASE_URL=https://your-domain.com \\
  -e ASK4ME_API_KEY=your-key \\
  -e ASK4ME_SERVERCHAN_SENDKEY=your-sendkey \\
  easychen/ask4me:latest`;

  const curlCmd = `curl -X POST http://localhost:8080/v1/ask \\
  -H 'Authorization: Bearer YOUR_KEY' \\
  -d '{"title":"Test","body":"Hello World","mcd":":::buttons\\n- [OK](ok)\\n:::"}'`;

  const steps = method === 'npm' ? t.install.npm.steps : t.install.docker.steps;
  const desc = method === 'npm' ? t.install.npm.desc : t.install.docker.desc;

  return (
    <section id="install" className="py-24 bg-slate-950 relative overflow-hidden">
      <div className="absolute right-0 bottom-0 w-[600px] h-[600px] bg-emerald-900/10 blur-[100px] rounded-full pointer-events-none" />

      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-16 items-start">

          <div>
            <h2 className="text-3xl md:text-4xl font-bold text-white mb-6">
              {t.install.titleStart} <span className="text-emerald-400">{t.install.titleHighlight}</span>
            </h2>
            <p className="text-slate-400 mb-8 text-lg">
              {desc}
            </p>

            <div className="space-y-4">
              {steps.map((step, i) => (
                <div key={i} className="flex items-start gap-4">
                  <div className="w-8 h-8 rounded-full bg-slate-800 flex items-center justify-center shrink-0 border border-slate-700 font-bold text-emerald-400">
                    {i + 1}
                  </div>
                  <div>
                    <h4 className="font-semibold text-white">{step.title}</h4>
                    <p className="text-slate-500 text-sm mt-1">{step.desc}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="bg-slate-950/50 backdrop-blur-xl border border-slate-800 rounded-2xl p-6 lg:p-8 shadow-2xl">
            {/* Tab switcher */}
            <div className="flex gap-2 mb-6">
              <button
                onClick={() => setMethod('npm')}
                className={`px-4 py-1.5 rounded-md text-sm font-medium transition-all ${
                  method === 'npm'
                    ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/40'
                    : 'text-slate-400 hover:text-slate-200 border border-transparent'
                }`}
              >
                NPM
              </button>
              <button
                onClick={() => setMethod('docker')}
                className={`px-4 py-1.5 rounded-md text-sm font-medium transition-all ${
                  method === 'docker'
                    ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/40'
                    : 'text-slate-400 hover:text-slate-200 border border-transparent'
                }`}
              >
                Docker
              </button>
            </div>

            {method === 'npm' ? (
              <>
                <CodeBlock id="install-cmd" label={t.install.npm.cmdInstall} command="npm install -g ask4me-server" />
                <CodeBlock id="run-cmd" label={t.install.npm.cmdRun} command="ask4me-server" />
                <CodeBlock id="curl-cmd" label={t.install.npm.cmdTest} command={curlCmd} />
              </>
            ) : (
              <>
                <CodeBlock id="docker-pull-cmd" label={t.install.docker.cmdPull} command="docker pull easychen/ask4me:latest" />
                <CodeBlock id="docker-run-cmd" label={t.install.docker.cmdRun} command={dockerRunCmd} />
                <CodeBlock id="docker-curl-cmd" label={t.install.docker.cmdTest} command={curlCmd} />
              </>
            )}
          </div>

        </div>
      </div>
    </section>
  );
};
