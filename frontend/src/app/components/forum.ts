import {
  ChangeDetectionStrategy,
  Component,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';
import { CommonModule, DatePipe } from '@angular/common';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatIconModule } from '@angular/material/icon';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { FormsModule } from '@angular/forms';

import { MarketService, describeHttpError } from '../services';
import { ForumPost } from '../models';

@Component({
  selector: 'fse-forum',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    CommonModule,
    DatePipe,
    MatCardModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatIconModule,
    MatSnackBarModule,
    FormsModule,
  ],
  templateUrl: './forum.html',
  styleUrl: './forum.scss',
})
export class ForumComponent {
  private readonly api = inject(MarketService);
  private readonly snackBar = inject(MatSnackBar);

  readonly trackedSymbol = input.required<string>();

  readonly posts = signal<ForumPost[]>([]);
  readonly loading = signal(false);

  author = '';
  title = '';
  content = '';

  constructor() {
    effect(() => this.refresh(this.trackedSymbol()));
  }

  refresh(symbol: string): void {
    this.loading.set(true);
    this.api.getForum(symbol || undefined, 50).subscribe({
      next: (posts) => {
        this.posts.set(posts ?? []);
        this.loading.set(false);
      },
      error: (err) => {
        this.loading.set(false);
        this.posts.set([]);
        this.notify('Failed to load forum: ' + describeHttpError(err));
      },
    });
  }

  submit(): void {
    const author = this.author.trim();
    const title = this.title.trim();
    const content = this.content.trim();
    if (!author || !title || !content) {
      this.notify('Please fill in Author, Title and Post content.');
      return;
    }
    this.api.createForumPost({ author, title, content, symbol: this.trackedSymbol() }).subscribe({
      next: () => {
        this.title = '';
        this.content = '';
        this.notify('Post broadcast successfully.');
        this.refresh(this.trackedSymbol());
      },
      error: (err) => this.notify('Failed to submit post: ' + describeHttpError(err)),
    });
  }

  vote(post: ForumPost): void {
    this.api.votePost(post.id, 1).subscribe({
      next: () => {
        this.posts.update((list) =>
          list.map((p) => (p.id === post.id ? { ...p, upvotes: p.upvotes + 1 } : p)),
        );
      },
      error: (err) => this.notify('Vote failed: ' + describeHttpError(err)),
    });
  }

  private notify(message: string): void {
    this.snackBar.open(message, 'OK', { duration: 3500, panelClass: ['snack-error'] });
  }
}
