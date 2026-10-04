export default function Home() {
  return (
    <main>
      <header>
        <p className="eyebrow">X Following Pruner</p>
        <h1>Follow fewer people, deliberately.</h1>
        <p className="lede">
          Review who you follow, then confirm each unfollow yourself. Nothing
          happens automatically.
        </p>
        <button disabled>Connect X — coming next</button>
      </header>

      <section aria-labelledby="how-it-works">
        <h2 id="how-it-works">How it will work</h2>
        <ol>
          <li>Connect your X account with OAuth.</li>
          <li>Load and filter the accounts you follow.</li>
          <li>Confirm each unfollow; the app queues it within X limits.</li>
        </ol>
      </section>

      <aside>
        <strong>Guardrail</strong>
        <p>
          X permits 50 unfollows per 15 minutes. This app will never bulk or
          automatically unfollow accounts.
        </p>
      </aside>
    </main>
  );
}
