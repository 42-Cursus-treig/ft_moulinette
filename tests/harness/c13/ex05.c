#include "ft_btree.h"
#include <unistd.h>

void	*btree_search_item(t_btree *root, void *data_ref,
			int (*cmpf)(void *, void *));
t_btree	*build_fixed_tree(void);
void	free_tree(t_btree *root);

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static int	ft_strcmp(void *a, void *b)
{
	char	*s1;
	char	*s2;
	int		i;

	s1 = (char *)a;
	s2 = (char *)b;
	i = 0;
	while (s1[i] && s1[i] == s2[i])
		i++;
	return ((unsigned char)s1[i] - (unsigned char)s2[i]);
}

int	main(int argc, char **argv)
{
	t_btree	*root;
	void	*found;

	if (argc < 2)
		return (0);
	root = build_fixed_tree();
	found = btree_search_item(root, argv[1], &ft_strcmp);
	if (found)
		put_str((char *)found);
	else
		put_str("(null)");
	write(1, "\n", 1);
	free_tree(root);
	return (0);
}
