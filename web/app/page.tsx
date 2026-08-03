export default function Home() {
  return (
    <section>
      <h1 className="text-2xl font-bold mb-4">AI Cloud Ops</h1>
      <p className="text-gray-600 dark:text-gray-400 mb-6">
        Multi-account dashboard. Pick a view from the top nav.
      </p>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <a href="/alerts" className="block p-4 bg-white dark:bg-gray-900 rounded shadow hover:shadow-md">
          <div className="font-semibold">Alerts</div>
          <div className="text-sm text-gray-500">Live incident timeline + AI analysis</div>
        </a>
        <a href="/resources" className="block p-4 bg-white dark:bg-gray-900 rounded shadow hover:shadow-md">
          <div className="font-semibold">Resources</div>
          <div className="text-sm text-gray-500">ECS / RDS / SLB across accounts &amp; regions</div>
        </a>
        <a href="/rules" className="block p-4 bg-white dark:bg-gray-900 rounded shadow hover:shadow-md">
          <div className="font-semibold">Rules</div>
          <div className="text-sm text-gray-500">Alert thresholds + notification channels</div>
        </a>
      </div>
    </section>
  );
}